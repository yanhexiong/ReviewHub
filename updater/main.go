package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const (
	defaultShutdownTimeout = 90 * time.Second
	defaultHealthTimeout   = 75 * time.Second
)

type configuration struct {
	mode       string
	source     string
	target     string
	parentPID  int
	healthURL  string
	resultPath string
	logPath    string
	version    string
}

type updateResult struct {
	Status    string `json:"status"`
	Version   string `json:"version,omitempty"`
	Message   string `json:"message"`
	UpdatedAt int64  `json:"updatedAt"`
}

func main() {
	config, err := parseConfiguration()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := run(config); err != nil {
		writeResult(config.resultPath, updateResult{
			Status:    "failed",
			Version:   config.version,
			Message:   "Update could not be completed. The previous application remains available.",
			UpdatedAt: time.Now().UnixMilli(),
		})
		logLine(config.logPath, "update failed: "+err.Error())
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseConfiguration() (configuration, error) {
	var config configuration
	flag.StringVar(&config.mode, "mode", "update", "update or rollback")
	flag.StringVar(&config.source, "source", "", "verified update AppImage")
	flag.StringVar(&config.target, "target", "", "current AppImage")
	flag.IntVar(&config.parentPID, "pid", 0, "Review Hub process PID")
	flag.StringVar(&config.healthURL, "health-url", "", "local health endpoint")
	flag.StringVar(&config.resultPath, "result", "", "update result file")
	flag.StringVar(&config.logPath, "log", "", "update log file")
	flag.StringVar(&config.version, "version", "", "target version")
	flag.Parse()

	if config.mode != "update" && config.mode != "rollback" {
		return configuration{}, errors.New("mode must be update or rollback")
	}
	if config.target == "" || !filepath.IsAbs(config.target) {
		return configuration{}, errors.New("target must be an absolute AppImage path")
	}
	if config.parentPID <= 1 {
		return configuration{}, errors.New("pid must identify the running Review Hub process")
	}
	if config.healthURL == "" || config.resultPath == "" {
		return configuration{}, errors.New("health-url and result are required")
	}
	if config.mode == "update" && (config.source == "" || !filepath.IsAbs(config.source)) {
		return configuration{}, errors.New("source must be an absolute verified AppImage path")
	}
	return config, nil
}

func run(config configuration) error {
	logLine(config.logPath, "waiting for Review Hub to stop")
	if err := waitForProcessExit(config.parentPID, defaultShutdownTimeout); err != nil {
		return err
	}

	target := filepath.Clean(config.target)
	backup := target + ".backup"
	var previous string
	var err error
	if config.mode == "rollback" {
		previous, err = swapWithBackup(target, backup)
	} else {
		previous, err = installUpdate(filepath.Clean(config.source), target, backup)
	}
	if err != nil {
		return err
	}

	logLine(config.logPath, "starting updated Review Hub")
	child, err := startApplication(target, config.logPath)
	if err != nil {
		_ = restorePrevious(target, previous, backup, config.mode == "rollback")
		return fmt.Errorf("could not start the replacement application: %w", err)
	}
	if err := waitForHealth(config.healthURL, defaultHealthTimeout); err != nil {
		_ = child.Process.Kill()
		_ = child.Wait()
		if restoreErr := restorePrevious(target, previous, backup, config.mode == "rollback"); restoreErr != nil {
			return fmt.Errorf("replacement health check failed and rollback failed: %w", restoreErr)
		}
		if _, restartErr := startApplication(target, config.logPath); restartErr != nil {
			return fmt.Errorf("replacement health check failed and previous version could not restart: %w", restartErr)
		}
		return fmt.Errorf("replacement health check failed: %w", err)
	}

	writeResult(config.resultPath, updateResult{
		Status:    "succeeded",
		Version:   config.version,
		Message:   "The application was updated and passed its health check.",
		UpdatedAt: time.Now().UnixMilli(),
	})
	logLine(config.logPath, "update completed")
	return nil
}

func installUpdate(source, target, backup string) (string, error) {
	if err := sameDirectory(target, backup); err != nil {
		return "", err
	}
	if _, err := os.Stat(source); err != nil {
		return "", fmt.Errorf("verified update file is unavailable: %w", err)
	}
	staged := target + ".new-" + randomSuffix()
	if err := copyExecutable(source, staged); err != nil {
		return "", err
	}
	_ = os.Remove(source)
	if err := os.Remove(backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(staged)
		return "", fmt.Errorf("could not replace the previous backup: %w", err)
	}
	if err := os.Rename(target, backup); err != nil {
		_ = os.Remove(staged)
		return "", fmt.Errorf("could not move current AppImage to backup: %w", err)
	}
	if err := os.Rename(staged, target); err != nil {
		_ = os.Rename(backup, target)
		_ = os.Remove(staged)
		return "", fmt.Errorf("could not activate the verified update: %w", err)
	}
	return backup, nil
}

func swapWithBackup(target, backup string) (string, error) {
	if _, err := os.Stat(backup); err != nil {
		return "", errors.New("no local rollback version is available")
	}
	previous := target + ".rollback-" + randomSuffix()
	if err := os.Rename(target, previous); err != nil {
		return "", fmt.Errorf("could not stage current AppImage for rollback: %w", err)
	}
	if err := os.Rename(backup, target); err != nil {
		_ = os.Rename(previous, target)
		return "", fmt.Errorf("could not restore the previous AppImage: %w", err)
	}
	if err := os.Rename(previous, backup); err != nil {
		_ = os.Rename(target, previous)
		_ = os.Rename(backup, target)
		return "", fmt.Errorf("could not preserve the current AppImage as rollback backup: %w", err)
	}
	return backup, nil
}

func restorePrevious(target, previous, backup string, preserveFailedVersion bool) error {
	if previous == "" {
		return errors.New("previous AppImage is unavailable")
	}
	failed := target + ".failed-" + randomSuffix()
	if err := os.Rename(target, failed); err != nil {
		return err
	}
	if err := os.Rename(previous, target); err != nil {
		_ = os.Rename(failed, target)
		return err
	}
	if preserveFailedVersion {
		_ = os.Remove(backup)
		if err := os.Rename(failed, backup); err != nil {
			return err
		}
	} else if err := os.Remove(failed); err != nil {
		return err
	}
	return nil
}

func copyExecutable(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(destination)
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(destination)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(destination)
		return err
	}
	return nil
}

func sameDirectory(first, second string) error {
	if filepath.Dir(first) != filepath.Dir(second) {
		return errors.New("atomic replacement requires the same directory")
	}
	return nil
}

func startApplication(target, logPath string) (*exec.Cmd, error) {
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	command := exec.Command(target)
	command.Stdout = logFile
	command.Stderr = logFile
	command.Env = os.Environ()
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		return nil, err
	}
	return command, nil
}

func waitForProcessExit(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if err != nil && !errors.Is(err, syscall.EPERM) {
			return err
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("timed out waiting for Review Hub to stop")
}

func waitForHealth(url string, timeout time.Duration) error {
	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		response, err := client.Get(url)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				return nil
			}
		}
		time.Sleep(1 * time.Second)
	}
	return errors.New("new application did not pass its health check")
}

func writeResult(path string, result updateResult) {
	if path == "" {
		return
	}
	data, err := json.Marshal(result)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	temporary := path + ".tmp-" + randomSuffix()
	if err := os.WriteFile(temporary, data, 0600); err == nil {
		_ = os.Rename(temporary, path)
	}
}

func logLine(path, message string) {
	if path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	line := time.Now().UTC().Format(time.RFC3339) + " " + message + "\n"
	if file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600); err == nil {
		_, _ = file.WriteString(line)
		_ = file.Close()
	}
}

func randomSuffix() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err == nil {
		return hex.EncodeToString(bytes)
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}
