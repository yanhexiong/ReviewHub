package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
)

const (
	defaultDataDirectory      = "./data"
	defaultListenAddress      = "127.0.0.1:39100"
	defaultFrontendListenHost = "0.0.0.0"
	defaultFrontendListenPort = 3000
)

// Config is the small, explicit runtime surface used by the Go API process.
// It never includes browser-visible deployment paths or credentials.
type Config struct {
	ConfigPath          string
	ListenAddress       string
	FrontendListenHost  string
	FrontendListenPort  int
	DataDirectory       string
	DatabasePath        string
	SessionSecret       string
	EncryptionKey       string
	Production          bool
	MaxPDFBytes         int
	MaxImportBytes      int
	MaxProjectsPerUser  int
	MaxUsers            int
	DefaultAllowedRoots []string
	// AppImagePath and UpdaterPath are packaging-provided runtime locations.
	// They are not user configuration and are never read from .env files.
	AppImagePath string
	UpdaterPath  string
	ParentPID    int
}

type runtimeConfig struct {
	ListenHost    string `json:"listenHost"`
	ListenPort    int    `json:"listenPort"`
	Version       int    `json:"version"`
	DataDirectory string `json:"dataDirectory"`
	GoAPIListen   string `json:"goApiListen"`
	Defaults      struct {
		AllowedRoots       []string `json:"allowedRoots"`
		MaxPDFBytes        int      `json:"maxPdfBytes"`
		MaxImportBytes     int      `json:"maxImportBytes"`
		MaxProjectsPerUser int      `json:"maxProjectsPerUser"`
		MaxUsers           int      `json:"maxUsers"`
	} `json:"defaults"`
}

type keyState struct {
	Version int `json:"version"`
}

type secretMaterial struct {
	EncryptionKey string
	SessionSecret string
}

// Load reads the persisted runtime configuration and creates static safe
// defaults on first use. Application configuration is never imported from
// .env files or environment variables.
func Load() (Config, error) {
	configPath, err := runtimeConfigPath()
	if err != nil {
		return Config{}, err
	}
	if err := ensurePrivateDirectory(filepath.Dir(configPath)); err != nil {
		return Config{}, err
	}
	runtime, err := loadRuntimeConfig(configPath)
	if err != nil {
		return Config{}, err
	}
	dataDirectory := strings.TrimSpace(runtime.DataDirectory)
	if dataDirectory == "" {
		dataDirectory = defaultDataDirectory
	}
	absDataDirectory, err := filepath.Abs(dataDirectory)
	if err != nil {
		return Config{}, fmt.Errorf("resolve data directory: %w", err)
	}

	listenAddress := strings.TrimSpace(runtime.GoAPIListen)
	if listenAddress == "" {
		listenAddress = defaultListenAddress
	}
	if _, _, err := net.SplitHostPort(listenAddress); err != nil {
		return Config{}, fmt.Errorf("invalid goApiListen in runtime-config.json: %w", err)
	}
	frontendHost := strings.TrimSpace(runtime.ListenHost)
	if frontendHost == "" {
		frontendHost = defaultFrontendListenHost
	}
	frontendPort := runtime.ListenPort
	if frontendPort == 0 {
		frontendPort = defaultFrontendListenPort
	}
	frontendHost, frontendPort, err = normalizeFrontendListener(frontendHost, frontendPort)
	if err != nil {
		return Config{}, fmt.Errorf("invalid frontend listener in runtime-config.json: %w", err)
	}

	maxUsers := positiveInteger(strconv.Itoa(runtime.Defaults.MaxUsers), 1000)
	material, err := loadSecretMaterial(filepath.Dir(configPath))
	if err != nil {
		return Config{}, err
	}
	return Config{
		ConfigPath:          configPath,
		ListenAddress:       listenAddress,
		FrontendListenHost:  frontendHost,
		FrontendListenPort:  frontendPort,
		DataDirectory:       absDataDirectory,
		DatabasePath:        filepath.Join(absDataDirectory, "paper-review.db"),
		SessionSecret:       material.SessionSecret,
		EncryptionKey:       material.EncryptionKey,
		Production:          os.Getenv("NODE_ENV") == "production",
		MaxPDFBytes:         positiveInteger(strconv.Itoa(runtime.Defaults.MaxPDFBytes), 104_857_600),
		MaxImportBytes:      positiveInteger(strconv.Itoa(runtime.Defaults.MaxImportBytes), 1_073_741_824),
		MaxProjectsPerUser:  nonNegativeInteger(runtime.Defaults.MaxProjectsPerUser, 20),
		MaxUsers:            maxUsers,
		DefaultAllowedRoots: append([]string(nil), runtime.Defaults.AllowedRoots...),
		AppImagePath:        strings.TrimSpace(os.Getenv("REVIEW_HUB_APPIMAGE_PATH")),
		UpdaterPath:         strings.TrimSpace(os.Getenv("REVIEW_HUB_UPDATER_BIN")),
		ParentPID:           os.Getppid(),
	}, nil
}

func runtimeConfigPath() (string, error) {
	configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory for runtime configuration: %w", err)
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "review-hub", "runtime-config.json"), nil
}

func loadRuntimeConfig(path string) (runtimeConfig, error) {
	contents, err := os.ReadFile(path)
	if err == nil {
		var configuration runtimeConfig
		if err := json.Unmarshal(contents, &configuration); err != nil {
			return runtimeConfig{}, fmt.Errorf("read runtime-config.json: %w", err)
		}
		return configuration, nil
	}
	if !os.IsNotExist(err) {
		return runtimeConfig{}, fmt.Errorf("read runtime-config.json: %w", err)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return runtimeConfig{}, fmt.Errorf("resolve working directory: %w", err)
	}
	configuration := runtimeConfig{
		Version:       1,
		DataDirectory: filepath.Join(workingDirectory, "data"),
		GoAPIListen:   defaultListenAddress,
		ListenHost:    defaultFrontendListenHost,
		ListenPort:    defaultFrontendListenPort,
	}
	configuration.Defaults.AllowedRoots = []string{workingDirectory}
	configuration.Defaults.MaxPDFBytes = 104_857_600
	configuration.Defaults.MaxImportBytes = 1_073_741_824
	configuration.Defaults.MaxProjectsPerUser = 20
	configuration.Defaults.MaxUsers = 1000
	encoded, err := json.MarshalIndent(configuration, "", "  ")
	if err != nil {
		return runtimeConfig{}, err
	}
	if err := writePrivateFile(path, append(encoded, '\n')); err != nil {
		return runtimeConfig{}, err
	}
	return configuration, nil
}

// SaveFrontendListener updates the public page listener while preserving all
// other bootstrap settings and secret files. The page process reads these
// values on its next start.
func (configuration Config) SaveFrontendListener(host string, port int) error {
	host, port, err := normalizeFrontendListener(host, port)
	if err != nil {
		return err
	}
	runtime, err := loadRuntimeConfig(configuration.ConfigPath)
	if err != nil {
		return err
	}
	runtime.ListenHost = host
	runtime.ListenPort = port
	encoded, err := json.MarshalIndent(runtime, "", "  ")
	if err != nil {
		return fmt.Errorf("encode runtime-config.json: %w", err)
	}
	return writePrivateFile(configuration.ConfigPath, append(encoded, '\n'))
}

func normalizeFrontendListener(host string, port int) (string, int, error) {
	host = strings.TrimSpace(host)
	if !domain.ValidListener(host, port) {
		return "", 0, fmt.Errorf("invalid listener host")
	}
	return host, port, nil
}

// loadSecretMaterial preserves an existing encryption key while rotating the
// session-signing key. A deployment created before the key split may have used
// the session secret for credential encryption; retaining it avoids making
// stored credentials unreadable during the one-time transition.
func loadSecretMaterial(configDirectory string) (secretMaterial, error) {
	if err := ensurePrivateDirectory(configDirectory); err != nil {
		return secretMaterial{}, err
	}
	sessionPath := filepath.Join(configDirectory, "session-secret")
	encryptionPath := filepath.Join(configDirectory, "encryption-key")
	statePath := filepath.Join(configDirectory, "key-state.json")

	state, err := loadKeyState(statePath)
	if err != nil {
		return secretMaterial{}, err
	}
	sessionSecret, hasSession, err := readSecret(sessionPath, "session-secret")
	if err != nil {
		return secretMaterial{}, err
	}
	encryptionKey, hasEncryption, err := readSecret(encryptionPath, "encryption-key")
	if err != nil {
		return secretMaterial{}, err
	}
	if state {
		if !hasSession || !hasEncryption {
			return secretMaterial{}, fmt.Errorf("key files are incomplete")
		}
		return secretMaterial{EncryptionKey: encryptionKey, SessionSecret: sessionSecret}, nil
	}

	if !hasEncryption {
		if hasSession {
			encryptionKey = sessionSecret
		} else {
			encryptionKey, err = generatedSecret()
			if err != nil {
				return secretMaterial{}, err
			}
		}
		if err := writePrivateFile(encryptionPath, []byte(encryptionKey+"\n")); err != nil {
			return secretMaterial{}, err
		}
	}
	newSessionSecret, err := generatedSecret()
	if err != nil {
		return secretMaterial{}, err
	}
	if err := writePrivateFile(sessionPath, []byte(newSessionSecret+"\n")); err != nil {
		return secretMaterial{}, err
	}
	stateContents, err := json.Marshal(keyState{Version: 1})
	if err != nil {
		return secretMaterial{}, err
	}
	if err := writePrivateFile(statePath, append(stateContents, '\n')); err != nil {
		return secretMaterial{}, err
	}
	return secretMaterial{EncryptionKey: encryptionKey, SessionSecret: newSessionSecret}, nil
}

func generatedSecret() (string, error) {
	bytes := make([]byte, 48)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("create secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func readSecret(path, label string) (string, bool, error) {
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read %s: %w", label, err)
	}
	secret := strings.TrimSpace(string(contents))
	if len(secret) < 16 {
		return "", false, fmt.Errorf("%s is invalid", label)
	}
	return secret, true, nil
}

func loadKeyState(path string) (bool, error) {
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read key-state.json: %w", err)
	}
	var state keyState
	if err := json.Unmarshal(contents, &state); err != nil || state.Version != 1 {
		return false, fmt.Errorf("key-state.json is invalid")
	}
	return true, nil
}

func writePrivateFile(path string, contents []byte) error {
	if err := ensurePrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".review-hub-config-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func ensurePrivateDirectory(directory string) error {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create runtime configuration directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("protect runtime configuration directory: %w", err)
	}
	return nil
}

func positiveInteger(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func nonNegativeInteger(value, fallback int) int {
	if value >= 0 {
		return value
	}
	return fallback
}
