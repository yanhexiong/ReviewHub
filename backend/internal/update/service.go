package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/go-github/v65/github"
)

const (
	OfficialRepository = "yanhexiong/ReviewHub"
	officialOwner      = "yanhexiong"
	officialName       = "ReviewHub"

	supportedPlatform      = "linux"
	supportedArchitecture  = "x86_64"
	MaxOfflinePackageBytes = int64(2 * 1024 * 1024 * 1024)
	maxManifestBytes       = int64(64 * 1024)
	maxChecksumBytes       = int64(1024 * 1024)
	maxRollbackVersions    = 3
)

// BuildVersion is set by the release workflow with -ldflags. Development
// binaries retain a clearly non-release version and therefore cannot be
// confused with an official AppImage release.
var BuildVersion = "0.0.0-dev"

type Runtime struct {
	AppImagePath  string
	UpdaterPath   string
	DataDirectory string
	ParentPID     int
}

type RuntimeStatus struct {
	Available bool    `json:"available"`
	Reason    *string `json:"reason"`
}

type ReleaseSummary struct {
	Version          string  `json:"version"`
	Name             string  `json:"name"`
	Notes            string  `json:"notes"`
	PublishedAt      *string `json:"publishedAt"`
	HasAppImage      bool    `json:"hasAppImage"`
	HasOfflineBundle bool    `json:"hasOfflineBundle"`
	HasUpdate        bool    `json:"hasUpdate,omitempty"`
}

type Result struct {
	Status    string `json:"status"`
	Version   string `json:"version,omitempty"`
	Message   string `json:"message"`
	UpdatedAt int64  `json:"updatedAt"`
}

type Overview struct {
	Repository             string           `json:"repository"`
	RepositoryURL          string           `json:"repositoryUrl"`
	Runtime                RuntimeStatus    `json:"runtime"`
	CurrentVersion         string           `json:"currentVersion"`
	Latest                 *ReleaseSummary  `json:"latest"`
	RollbackVersions       []ReleaseSummary `json:"rollbackVersions"`
	LocalRollbackAvailable bool             `json:"localRollbackAvailable"`
	Result                 *Result          `json:"result"`
	CheckError             *string          `json:"checkError"`
}

type offlineManifest struct {
	Format       string `json:"format"`
	Version      string `json:"version"`
	Platform     string `json:"platform"`
	Architecture string `json:"architecture"`
	AppImage     string `json:"appImage"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
}

type stagedRelease struct {
	Path    string
	Version string
}

type releaseClient interface {
	Latest(context.Context) (*github.RepositoryRelease, error)
	Recent(context.Context, int) ([]*github.RepositoryRelease, error)
	Download(context.Context, int64) (io.ReadCloser, error)
}

type githubReleaseClient struct {
	client         *github.Client
	downloadClient *http.Client
}

func newGitHubReleaseClient() *githubReleaseClient {
	client := github.NewClient(&http.Client{Timeout: 20 * time.Second})
	client.UserAgent = "Review-Hub-Updater"
	return &githubReleaseClient{
		client:         client,
		downloadClient: &http.Client{Timeout: 10 * time.Minute},
	}
}

func (client *githubReleaseClient) Latest(ctx context.Context) (*github.RepositoryRelease, error) {
	release, _, err := client.client.Repositories.GetLatestRelease(ctx, officialOwner, officialName)
	return release, err
}

func (client *githubReleaseClient) Recent(ctx context.Context, limit int) ([]*github.RepositoryRelease, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}
	releases, _, err := client.client.Repositories.ListReleases(ctx, officialOwner, officialName, &github.ListOptions{PerPage: limit})
	return releases, err
}

func (client *githubReleaseClient) Download(ctx context.Context, assetID int64) (io.ReadCloser, error) {
	reader, redirect, err := client.client.Repositories.DownloadReleaseAsset(ctx, officialOwner, officialName, assetID, client.downloadClient)
	if err != nil {
		return nil, err
	}
	if reader == nil || redirect != "" {
		return nil, errors.New("GitHub release asset did not return a readable response")
	}
	return reader, nil
}

type Service struct {
	runtime Runtime
	client  releaseClient
}

func New(runtime Runtime) *Service {
	return newWithClient(runtime, newGitHubReleaseClient())
}

func newWithClient(runtime Runtime, client releaseClient) *Service {
	return &Service{runtime: runtime, client: client}
}

func (service *Service) Overview(ctx context.Context) Overview {
	runtime := service.runtimeStatus()
	result := service.readResult()
	overview := Overview{
		Repository:             OfficialRepository,
		RepositoryURL:          "https://github.com/" + OfficialRepository,
		Runtime:                runtime,
		CurrentVersion:         BuildVersion,
		RollbackVersions:       []ReleaseSummary{},
		LocalRollbackAvailable: service.localRollbackAvailable(),
		Result:                 result,
	}
	latest, err := service.client.Latest(ctx)
	if err != nil {
		message := "无法从官方发行源检查更新，请稍后重试。"
		overview.CheckError = &message
		return overview
	}
	summary, err := releaseSummary(latest)
	if err != nil {
		message := "官方发行源返回了无效的版本信息。"
		overview.CheckError = &message
		return overview
	}
	if current, err := strictVersion(BuildVersion); err == nil {
		if target, err := strictVersion(summary.Version); err == nil {
			summary.HasUpdate = current.LessThan(target)
		}
	}
	overview.Latest = &summary
	if current, err := strictVersion(BuildVersion); err == nil {
		if releases, err := service.client.Recent(ctx, 15); err == nil {
			for _, release := range releases {
				candidate, candidateErr := releaseSummary(release)
				if candidateErr != nil {
					continue
				}
				version, versionErr := strictVersion(candidate.Version)
				if versionErr != nil || !version.LessThan(current) {
					continue
				}
				overview.RollbackVersions = append(overview.RollbackVersions, candidate)
				if len(overview.RollbackVersions) == maxRollbackVersions {
					break
				}
			}
		}
	}
	return overview
}

func (service *Service) QueueLatest(ctx context.Context, healthURL string) (string, error) {
	if err := service.requireRuntime(); err != nil {
		return "", err
	}
	release, err := service.client.Latest(ctx)
	if err != nil {
		return "", updateError("UPDATE_RELEASE_UNAVAILABLE", "无法从官方发行源获取更新。")
	}
	summary, err := releaseSummary(release)
	if err != nil {
		return "", updateError("UPDATE_RELEASE_UNAVAILABLE", "官方发行版本无效。")
	}
	if err := service.requireNewer(summary.Version); err != nil {
		return "", err
	}
	staged, err := service.stageRelease(ctx, release)
	if err != nil {
		return "", err
	}
	if err := service.queueReplacement(staged, healthURL, "update"); err != nil {
		_ = os.Remove(staged.Path)
		return "", err
	}
	return staged.Version, nil
}

func (service *Service) QueueOffline(ctx context.Context, packageReader io.Reader, healthURL string) (string, error) {
	if err := service.requireRuntime(); err != nil {
		return "", err
	}
	staged, err := service.stageOfflineBundle(ctx, packageReader)
	if err != nil {
		return "", err
	}
	if err := service.requireNewer(staged.Version); err != nil {
		_ = os.Remove(staged.Path)
		return "", err
	}
	if err := service.queueReplacement(staged, healthURL, "update"); err != nil {
		_ = os.Remove(staged.Path)
		return "", err
	}
	return staged.Version, nil
}

func (service *Service) QueueReleaseRollback(ctx context.Context, requestedVersion, healthURL string) (string, error) {
	if err := service.requireRuntime(); err != nil {
		return "", err
	}
	target, err := strictVersion(requestedVersion)
	if err != nil {
		return "", updateError("INVALID_ROLLBACK_VERSION", "回退版本无效。")
	}
	current, err := strictVersion(BuildVersion)
	if err != nil || !target.LessThan(current) {
		return "", updateError("INVALID_ROLLBACK_VERSION", "只能回退到较早的稳定版本。")
	}
	releases, err := service.client.Recent(ctx, 15)
	if err != nil {
		return "", updateError("UPDATE_RELEASE_UNAVAILABLE", "无法读取可回退的官方发行版本。")
	}
	var selected *github.RepositoryRelease
	allowed := 0
	for _, release := range releases {
		summary, summaryErr := releaseSummary(release)
		if summaryErr != nil {
			continue
		}
		version, versionErr := strictVersion(summary.Version)
		if versionErr != nil || !version.LessThan(current) {
			continue
		}
		allowed++
		if version.Equal(target) {
			selected = release
			break
		}
		if allowed == maxRollbackVersions {
			break
		}
	}
	if selected == nil {
		return "", updateError("INVALID_ROLLBACK_VERSION", "该版本不在可回退范围内。")
	}
	staged, err := service.stageRelease(ctx, selected)
	if err != nil {
		return "", err
	}
	if err := service.queueReplacement(staged, healthURL, "update"); err != nil {
		_ = os.Remove(staged.Path)
		return "", err
	}
	return staged.Version, nil
}

func (service *Service) QueueLocalRollback(healthURL string) error {
	if err := service.requireRuntime(); err != nil {
		return err
	}
	backup := service.runtime.AppImagePath + ".backup"
	if _, err := os.Stat(backup); err != nil {
		return updateError("ROLLBACK_UNAVAILABLE", "没有可用的本地回退版本。")
	}
	return service.startHelper("rollback", "", "previous", healthURL)
}

func (service *Service) runtimeStatus() RuntimeStatus {
	if err := service.requireRuntime(); err != nil {
		message := err.Error()
		return RuntimeStatus{Reason: &message}
	}
	return RuntimeStatus{Available: true}
}

func (service *Service) requireRuntime() error {
	if service.runtime.AppImagePath == "" || service.runtime.UpdaterPath == "" {
		return updateError("UPDATE_UNSUPPORTED_RUNTIME", "当前运行方式不支持自动更新，请使用正式 AppImage 发行包启动应用。")
	}
	if service.runtime.ParentPID <= 1 {
		return updateError("UPDATE_UNSUPPORTED_RUNTIME", "当前运行方式不支持自动更新。")
	}
	for _, candidate := range []struct {
		path       string
		executable bool
	}{
		{service.runtime.AppImagePath, true},
		{service.runtime.UpdaterPath, true},
	} {
		info, err := os.Stat(candidate.path)
		if err != nil || !info.Mode().IsRegular() || (candidate.executable && info.Mode()&0o111 == 0) {
			return updateError("UPDATE_UNSUPPORTED_RUNTIME", "当前发行包位置不可写或更新助手不可用，无法自动替换。")
		}
	}
	if _, err := os.Stat(service.updateDirectory()); err != nil && !os.IsNotExist(err) {
		return updateError("UPDATE_UNSUPPORTED_RUNTIME", "更新暂存目录不可用。")
	}
	return nil
}

func (service *Service) updateDirectory() string {
	return filepath.Join(service.runtime.DataDirectory, "updates")
}

func (service *Service) resultPath() string {
	return filepath.Join(service.updateDirectory(), "last-result.json")
}

func (service *Service) readResult() *Result {
	contents, err := os.ReadFile(service.resultPath())
	if err != nil {
		return nil
	}
	var result Result
	if json.Unmarshal(contents, &result) != nil || (result.Status != "succeeded" && result.Status != "failed") {
		return nil
	}
	return &result
}

func (service *Service) localRollbackAvailable() bool {
	if service.runtime.AppImagePath == "" {
		return false
	}
	info, err := os.Stat(service.runtime.AppImagePath + ".backup")
	return err == nil && info.Mode().IsRegular()
}

func releaseSummary(release *github.RepositoryRelease) (ReleaseSummary, error) {
	if release == nil || release.GetDraft() || release.GetPrerelease() {
		return ReleaseSummary{}, errors.New("release is not stable")
	}
	version, err := normalizeVersion(release.GetTagName())
	if err != nil {
		return ReleaseSummary{}, err
	}
	assets := release.Assets
	var publishedAt *string
	if release.PublishedAt != nil {
		formatted := release.PublishedAt.Format(time.RFC3339)
		publishedAt = &formatted
	}
	return ReleaseSummary{
		Version:          version,
		Name:             firstNonEmpty(release.GetName(), "Review Hub v"+version),
		Notes:            firstNonEmpty(release.GetBody(), "未提供发行说明。"),
		PublishedAt:      publishedAt,
		HasAppImage:      findAsset(assets, expectedAppImageName(version)) != nil && findAsset(assets, "SHA256SUMS") != nil,
		HasOfflineBundle: findAsset(assets, expectedOfflineBundleName(version)) != nil,
	}, nil
}

func strictVersion(value string) (*semver.Version, error) {
	return semver.StrictNewVersion(strings.TrimPrefix(strings.TrimSpace(value), "v"))
}

func normalizeVersion(value string) (string, error) {
	version, err := strictVersion(value)
	if err != nil {
		return "", err
	}
	if version.Prerelease() != "" {
		return "", errors.New("pre-release versions are not accepted")
	}
	return version.String(), nil
}

func expectedAppImageName(version string) string {
	return "ReviewHub-v" + version + "-" + supportedArchitecture + ".AppImage"
}

func expectedOfflineBundleName(version string) string {
	return "ReviewHub-v" + version + "-" + supportedArchitecture + ".update.tar.gz"
}

func findAsset(assets []*github.ReleaseAsset, name string) *github.ReleaseAsset {
	for _, asset := range assets {
		if asset != nil && asset.GetName() == name {
			return asset
		}
	}
	return nil
}

func (service *Service) stageRelease(ctx context.Context, release *github.RepositoryRelease) (stagedRelease, error) {
	summary, err := releaseSummary(release)
	if err != nil {
		return stagedRelease{}, updateError("UPDATE_RELEASE_UNAVAILABLE", "官方发行版本无效。")
	}
	image := findAsset(release.Assets, expectedAppImageName(summary.Version))
	checksums := findAsset(release.Assets, "SHA256SUMS")
	if image == nil || checksums == nil {
		return stagedRelease{}, updateError("UPDATE_ASSET_UNAVAILABLE", "官方发行版本缺少匹配的 AppImage 或校验清单。")
	}
	checksumReader, err := service.client.Download(ctx, checksums.GetID())
	if err != nil {
		return stagedRelease{}, updateError("UPDATE_DOWNLOAD_FAILED", "无法下载官方校验清单。")
	}
	checksumContents, readErr := readLimited(checksumReader, maxChecksumBytes)
	_ = checksumReader.Close()
	if readErr != nil {
		return stagedRelease{}, updateError("UPDATE_CHECKSUM_UNAVAILABLE", "官方校验清单无效。")
	}
	expected, ok := checksumForAsset(string(checksumContents), image.GetName())
	if !ok {
		return stagedRelease{}, updateError("UPDATE_CHECKSUM_UNAVAILABLE", "官方校验清单不包含目标 AppImage。")
	}
	reader, err := service.client.Download(ctx, image.GetID())
	if err != nil {
		return stagedRelease{}, updateError("UPDATE_DOWNLOAD_FAILED", "无法下载官方 AppImage。")
	}
	defer reader.Close()
	path, actual, size, err := service.writeStaged(reader, image.GetName())
	if err != nil {
		return stagedRelease{}, err
	}
	if image.GetSize() > 0 && size != int64(image.GetSize()) {
		_ = os.Remove(path)
		return stagedRelease{}, updateError("UPDATE_DOWNLOAD_FAILED", "下载的发行文件大小不匹配。")
	}
	if actual != expected {
		_ = os.Remove(path)
		return stagedRelease{}, updateError("UPDATE_CHECKSUM_MISMATCH", "发行文件校验失败，未安排更新。")
	}
	return stagedRelease{Path: path, Version: summary.Version}, nil
}

func (service *Service) stageOfflineBundle(ctx context.Context, source io.Reader) (stagedRelease, error) {
	if err := ctx.Err(); err != nil {
		return stagedRelease{}, err
	}
	gzipReader, err := gzip.NewReader(io.LimitReader(source, MaxOfflinePackageBytes+1))
	if err != nil {
		return stagedRelease{}, updateError("OFFLINE_PACKAGE_INVALID", "离线更新包不是有效的 gzip 归档。")
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	first, err := tarReader.Next()
	if err != nil || first == nil || first.Typeflag != tar.TypeReg || first.Name != "manifest.json" || first.Size < 2 || first.Size > maxManifestBytes {
		return stagedRelease{}, updateError("OFFLINE_PACKAGE_INVALID", "离线更新包缺少有效的 manifest.json。")
	}
	manifestContents, err := readLimited(io.LimitReader(tarReader, first.Size), maxManifestBytes)
	if err != nil || int64(len(manifestContents)) != first.Size {
		return stagedRelease{}, updateError("OFFLINE_PACKAGE_INVALID", "离线更新包的 manifest.json 无效。")
	}
	var manifest offlineManifest
	if json.Unmarshal(manifestContents, &manifest) != nil || !validOfflineManifest(manifest) {
		return stagedRelease{}, updateError("OFFLINE_PACKAGE_INVALID", "离线更新包的 manifest.json 不受支持。")
	}
	entry, err := tarReader.Next()
	if err != nil || entry == nil || entry.Typeflag != tar.TypeReg || entry.Name != manifest.AppImage || entry.Size != manifest.Size || entry.Size < 1 || entry.Size > MaxOfflinePackageBytes {
		return stagedRelease{}, updateError("OFFLINE_PACKAGE_INVALID", "离线更新包缺少匹配的 AppImage。")
	}
	path, actual, size, err := service.writeStaged(io.LimitReader(tarReader, entry.Size), manifest.AppImage)
	if err != nil {
		return stagedRelease{}, err
	}
	if size != manifest.Size || !strings.EqualFold(actual, manifest.SHA256) {
		_ = os.Remove(path)
		return stagedRelease{}, updateError("UPDATE_CHECKSUM_MISMATCH", "离线更新包校验失败，未安排更新。")
	}
	if extra, err := tarReader.Next(); err != io.EOF || extra != nil {
		_ = os.Remove(path)
		return stagedRelease{}, updateError("OFFLINE_PACKAGE_INVALID", "离线更新包包含不受支持的额外文件。")
	}
	return stagedRelease{Path: path, Version: manifest.Version}, nil
}

func validOfflineManifest(manifest offlineManifest) bool {
	version, err := normalizeVersion(manifest.Version)
	if err != nil || version != manifest.Version || manifest.Format != "review-hub-offline-update/v1" ||
		manifest.Platform != supportedPlatform || manifest.Architecture != supportedArchitecture ||
		manifest.AppImage != expectedAppImageName(manifest.Version) || manifest.Size < 1 || manifest.Size > MaxOfflinePackageBytes {
		return false
	}
	decoded, err := hex.DecodeString(manifest.SHA256)
	return err == nil && len(decoded) == sha256.Size
}

func (service *Service) writeStaged(reader io.Reader, name string) (string, string, int64, error) {
	if name != filepath.Base(name) || name == "." || name == ".." {
		return "", "", 0, updateError("UPDATE_ASSET_UNAVAILABLE", "发行文件名无效。")
	}
	if err := os.MkdirAll(service.updateDirectory(), 0o700); err != nil {
		return "", "", 0, updateError("UPDATE_STAGING_FAILED", "无法创建更新暂存目录。")
	}
	file, err := os.CreateTemp(service.updateDirectory(), "."+name+"-")
	if err != nil {
		return "", "", 0, updateError("UPDATE_STAGING_FAILED", "无法创建更新暂存文件。")
	}
	path := file.Name()
	defer func() {
		_ = file.Close()
	}()
	if err := file.Chmod(0o700); err != nil {
		_ = os.Remove(path)
		return "", "", 0, updateError("UPDATE_STAGING_FAILED", "无法保护更新暂存文件。")
	}
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(reader, MaxOfflinePackageBytes+1))
	if err != nil || size > MaxOfflinePackageBytes || file.Sync() != nil || file.Close() != nil {
		_ = os.Remove(path)
		return "", "", 0, updateError("UPDATE_STAGING_FAILED", "无法保存完整的更新文件。")
	}
	return path, hex.EncodeToString(hash.Sum(nil)), size, nil
}

func checksumForAsset(contents, name string) (string, bool) {
	for _, line := range strings.Split(contents, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		digest, err := hex.DecodeString(fields[0])
		if err == nil && len(digest) == sha256.Size {
			return strings.ToLower(fields[0]), true
		}
	}
	return "", false
}

func firstNonEmpty(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}

func readLimited(reader io.Reader, maximum int64) ([]byte, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil || int64(len(contents)) > maximum {
		return nil, errors.New("input exceeds limit")
	}
	return contents, nil
}

func (service *Service) requireNewer(version string) error {
	current, currentErr := strictVersion(BuildVersion)
	target, targetErr := strictVersion(version)
	if currentErr != nil || targetErr != nil || !current.LessThan(target) {
		return updateError("ALREADY_UP_TO_DATE", "当前已经是最新版本或离线包不是较新版本。")
	}
	return nil
}

func (service *Service) queueReplacement(staged stagedRelease, healthURL, mode string) error {
	if healthURL == "" {
		return updateError("UPDATE_UNSUPPORTED_RUNTIME", "当前服务健康检查地址不可用。")
	}
	if err := service.startHelper(mode, staged.Path, staged.Version, healthURL); err != nil {
		return err
	}
	go func(parentPID int) {
		time.Sleep(1500 * time.Millisecond)
		_ = syscall.Kill(parentPID, syscall.SIGTERM)
	}(service.runtime.ParentPID)
	return nil
}

func (service *Service) startHelper(mode, source, version, healthURL string) error {
	if err := os.MkdirAll(service.updateDirectory(), 0o700); err != nil {
		return updateError("UPDATE_HELPER_UNAVAILABLE", "更新助手无法访问暂存目录。")
	}
	arguments := []string{
		"--mode", mode,
		"--target", service.runtime.AppImagePath,
		"--pid", fmt.Sprintf("%d", service.runtime.ParentPID),
		"--health-url", healthURL,
		"--result", service.resultPath(),
		"--log", filepath.Join(service.updateDirectory(), "updater.log"),
		"--version", version,
	}
	if mode == "update" {
		arguments = append(arguments, "--source", source)
	}
	command := exec.Command(service.runtime.UpdaterPath, arguments...)
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	command.Stdout = nil
	command.Stderr = nil
	if err := command.Start(); err != nil {
		return updateError("UPDATE_HELPER_UNAVAILABLE", "更新助手无法启动，未替换当前版本。")
	}
	return nil
}

type serviceError struct {
	code    string
	message string
}

func (error *serviceError) Error() string { return error.message }

func updateError(code, message string) *serviceError {
	return &serviceError{code: code, message: message}
}

func ErrorCode(err error) string {
	var updateErr *serviceError
	if errors.As(err, &updateErr) {
		return updateErr.code
	}
	return "UPDATE_FAILED"
}
