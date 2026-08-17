package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMigratesLegacySessionSecretWithoutUsingEnvironmentInput(t *testing.T) {
	root := t.TempDir()
	configHome := filepath.Join(root, "config")
	configFile := filepath.Join(configHome, "review-hub", "runtime-config.json")
	dataDirectory := filepath.Join(root, "data")
	legacySecret := "legacy-session-secret-used-for-existing-ciphertext"
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("SESSION_SECRET", "user-supplied-session-secret-must-be-ignored")
	if err := writeRuntimeConfig(configFile, dataDirectory); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(configFile), "session-secret"), []byte(legacySecret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if configuration.DataDirectory != dataDirectory || configuration.SessionSecret == legacySecret {
		t.Fatalf("unexpected configuration: %#v", configuration)
	}
	if configuration.SessionSecret == os.Getenv("SESSION_SECRET") || len(configuration.SessionSecret) != 64 {
		t.Fatalf("session secret was not randomly rotated: %#v", configuration)
	}
	for _, file := range []string{
		configFile,
		filepath.Join(filepath.Dir(configFile), "session-secret"),
		filepath.Join(filepath.Dir(configFile), "encryption-key"),
		filepath.Join(filepath.Dir(configFile), "key-state.json"),
	} {
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", file, info.Mode().Perm())
		}
	}
	encryptionKey, err := os.ReadFile(filepath.Join(filepath.Dir(configFile), "encryption-key"))
	if err != nil || string(encryptionKey) != legacySecret+"\n" {
		t.Fatalf("legacy encryption key was not preserved: %q, %v", encryptionKey, err)
	}
	directory, err := os.Stat(filepath.Dir(configFile))
	if err != nil {
		t.Fatal(err)
	}
	if directory.Mode().Perm() != 0o700 {
		t.Fatalf("configuration directory mode = %o, want 700", directory.Mode().Perm())
	}

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DataDirectory != dataDirectory || loaded.SessionSecret != configuration.SessionSecret {
		t.Fatalf("persisted configuration was not reused: %#v", loaded)
	}
}

func TestLoadUsesStaticDefaultsInsteadOfApplicationEnvironment(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("PAPER_REVIEW_DATA_DIR", filepath.Join(root, "ignored-data"))
	t.Setenv("PAPER_REVIEW_MAX_USERS", "42")
	t.Setenv("PAPER_REVIEW_ALLOWED_ROOTS", filepath.Join(root, "ignored-root"))

	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if configuration.DataDirectory != filepath.Join(workingDirectory, "data") || configuration.MaxUsers != 1000 {
		t.Fatalf("application environment leaked into configuration: %#v", configuration)
	}
	if len(configuration.DefaultAllowedRoots) != 1 || configuration.DefaultAllowedRoots[0] != workingDirectory {
		t.Fatalf("unexpected static allowed roots: %#v", configuration.DefaultAllowedRoots)
	}
}

func TestSaveFrontendListenerPersistsBootstrapSetting(t *testing.T) {
	root := t.TempDir()
	configHome := filepath.Join(root, "config")
	configFile := filepath.Join(configHome, "review-hub", "runtime-config.json")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	if err := writeRuntimeConfig(configFile, filepath.Join(root, "data")); err != nil {
		t.Fatal(err)
	}

	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if configuration.FrontendListenHost != defaultFrontendListenHost || configuration.FrontendListenPort != defaultFrontendListenPort {
		t.Fatalf("legacy config did not receive listener defaults: %#v", configuration)
	}
	if err := configuration.SaveFrontendListener("127.0.0.1", 4321); err != nil {
		t.Fatal(err)
	}
	updated, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if updated.FrontendListenHost != "127.0.0.1" || updated.FrontendListenPort != 4321 {
		t.Fatalf("listener setting was not persisted: %#v", updated)
	}
	contents, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(contents, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["listenHost"] != "127.0.0.1" || saved["listenPort"] != float64(4321) {
		t.Fatalf("runtime config has unexpected listener fields: %#v", saved)
	}
	if err := configuration.SaveFrontendListener("not/a-host", 4321); err == nil {
		t.Fatal("invalid listener host was accepted")
	}
}

func writeRuntimeConfig(path, dataDirectory string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	configuration := runtimeConfig{
		Version:       1,
		DataDirectory: dataDirectory,
		GoAPIListen:   defaultListenAddress,
	}
	configuration.Defaults.AllowedRoots = []string{dataDirectory}
	configuration.Defaults.MaxPDFBytes = 104_857_600
	configuration.Defaults.MaxImportBytes = 1_073_741_824
	configuration.Defaults.MaxProjectsPerUser = 20
	configuration.Defaults.MaxUsers = 1000
	contents, err := json.Marshal(configuration)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(contents, '\n'), 0o600)
}
