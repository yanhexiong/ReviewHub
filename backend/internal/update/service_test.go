package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-github/v65/github"
)

type releaseClientStub struct {
	latest    *github.RepositoryRelease
	recent    []*github.RepositoryRelease
	latestErr error
	recentErr error
}

func (stub releaseClientStub) Latest(context.Context) (*github.RepositoryRelease, error) {
	return stub.latest, stub.latestErr
}

func (stub releaseClientStub) Recent(context.Context, int) ([]*github.RepositoryRelease, error) {
	return stub.recent, stub.recentErr
}

func (stub releaseClientStub) Download(context.Context, int64) (io.ReadCloser, error) {
	return nil, nil
}

func TestOfficialRepositoryIsFixed(t *testing.T) {
	if OfficialRepository != "yanhexiong/ReviewHub" {
		t.Fatalf("official repository = %q", OfficialRepository)
	}
	if officialOwner != "yanhexiong" || officialName != "ReviewHub" {
		t.Fatal("release client must not accept a browser-configurable repository")
	}
}

func TestReleaseSummaryRequiresExactAssets(t *testing.T) {
	release := &github.RepositoryRelease{
		TagName: github.String("v1.2.3"),
		Assets: []*github.ReleaseAsset{
			{Name: github.String("ReviewHub-v1.2.3-x86_64.AppImage")},
			{Name: github.String("SHA256SUMS")},
			{Name: github.String("ReviewHub-v1.2.3-x86_64.update.tar.gz")},
		},
	}
	summary, err := releaseSummary(release)
	if err != nil {
		t.Fatal(err)
	}
	if !summary.HasAppImage || !summary.HasOfflineBundle || summary.Version != "1.2.3" {
		t.Fatalf("unexpected release summary: %#v", summary)
	}

	_, err = releaseSummary(&github.RepositoryRelease{TagName: github.String("v1.2.4-rc.1")})
	if err == nil {
		t.Fatal("pre-release must be rejected")
	}
}

func TestOverviewSelectsOnlyNewerStableOfficialReleases(t *testing.T) {
	previous := BuildVersion
	BuildVersion = "1.2.3"
	t.Cleanup(func() { BuildVersion = previous })
	latest := releaseForVersion("1.2.4", false)
	service := newWithClient(Runtime{DataDirectory: t.TempDir()}, releaseClientStub{
		latest: latest,
		recent: []*github.RepositoryRelease{
			latest,
			releaseForVersion("1.2.3", false),
			releaseForVersion("1.2.2", false),
			releaseForVersion("1.2.1", true),
		},
	})
	overview := service.Overview(context.Background())
	if overview.Repository != OfficialRepository || overview.Latest == nil || !overview.Latest.HasUpdate {
		t.Fatalf("unexpected overview: %#v", overview)
	}
	if len(overview.RollbackVersions) != 1 || overview.RollbackVersions[0].Version != "1.2.2" {
		t.Fatalf("unexpected rollback list: %#v", overview.RollbackVersions)
	}
}

func TestOfflineBundleStagesOnlyVerifiedAppImage(t *testing.T) {
	payload := []byte("verified application image")
	version := "1.2.3"
	manifest := validManifest(version, payload)
	bundle := bundle(t, manifest, payload, false)

	service := newWithClient(Runtime{DataDirectory: t.TempDir()}, nil)
	staged, err := service.stageOfflineBundle(context.Background(), bytes.NewReader(bundle))
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(staged.Path)
	if staged.Version != version {
		t.Fatalf("staged version = %q", staged.Version)
	}
	contents, err := os.ReadFile(staged.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(contents, payload) {
		t.Fatalf("staged payload = %q", contents)
	}
	if filepath.Dir(staged.Path) != service.updateDirectory() {
		t.Fatalf("staged path escaped update directory: %q", staged.Path)
	}
}

func TestOfflineBundleRejectsChecksumMismatch(t *testing.T) {
	payload := []byte("untrusted application image")
	manifest := validManifest("1.2.3", payload)
	manifest.SHA256 = "00" + manifest.SHA256[2:]
	service := newWithClient(Runtime{DataDirectory: t.TempDir()}, nil)
	_, err := service.stageOfflineBundle(context.Background(), bytes.NewReader(bundle(t, manifest, payload, false)))
	if ErrorCode(err) != "UPDATE_CHECKSUM_MISMATCH" {
		t.Fatalf("error code = %q, error = %v", ErrorCode(err), err)
	}
}

func TestOfflineBundleRejectsUnsupportedArchitectureAndExtraFiles(t *testing.T) {
	payload := []byte("application image")
	manifest := validManifest("1.2.3", payload)
	manifest.Architecture = "arm64"
	service := newWithClient(Runtime{DataDirectory: t.TempDir()}, nil)
	_, err := service.stageOfflineBundle(context.Background(), bytes.NewReader(bundle(t, manifest, payload, false)))
	if ErrorCode(err) != "OFFLINE_PACKAGE_INVALID" {
		t.Fatalf("architecture error code = %q, error = %v", ErrorCode(err), err)
	}

	manifest = validManifest("1.2.3", payload)
	_, err = service.stageOfflineBundle(context.Background(), bytes.NewReader(bundle(t, manifest, payload, true)))
	if ErrorCode(err) != "OFFLINE_PACKAGE_INVALID" {
		t.Fatalf("extra file error code = %q, error = %v", ErrorCode(err), err)
	}
}

func TestRequireNewerRejectsDowngradeAndReplay(t *testing.T) {
	previous := BuildVersion
	BuildVersion = "1.2.3"
	t.Cleanup(func() { BuildVersion = previous })
	service := newWithClient(Runtime{}, nil)
	for _, target := range []string{"1.2.3", "1.2.2"} {
		if ErrorCode(service.requireNewer(target)) != "ALREADY_UP_TO_DATE" {
			t.Fatalf("target %q must be rejected", target)
		}
	}
	if err := service.requireNewer("1.2.4"); err != nil {
		t.Fatalf("newer version was rejected: %v", err)
	}
}

func validManifest(version string, payload []byte) offlineManifest {
	digest := sha256.Sum256(payload)
	return offlineManifest{
		Format:       "review-hub-offline-update/v1",
		Version:      version,
		Platform:     "linux",
		Architecture: "x86_64",
		AppImage:     expectedAppImageName(version),
		SHA256:       hex.EncodeToString(digest[:]),
		Size:         int64(len(payload)),
	}
}

func bundle(t *testing.T, manifest offlineManifest, payload []byte, extra bool) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	manifestContents, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeTarFile(t, tarWriter, "manifest.json", manifestContents)
	writeTarFile(t, tarWriter, manifest.AppImage, payload)
	if extra {
		writeTarFile(t, tarWriter, "extra.txt", []byte("not accepted"))
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func writeTarFile(t *testing.T, writer *tar.Writer, name string, contents []byte) {
	t.Helper()
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(contents)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(writer, bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
}

func releaseForVersion(version string, prerelease bool) *github.RepositoryRelease {
	return &github.RepositoryRelease{
		TagName:    github.String("v" + version),
		Prerelease: github.Bool(prerelease),
		Assets: []*github.ReleaseAsset{
			{Name: github.String(expectedAppImageName(version))},
			{Name: github.String("SHA256SUMS")},
			{Name: github.String(expectedOfflineBundleName(version))},
		},
	}
}
