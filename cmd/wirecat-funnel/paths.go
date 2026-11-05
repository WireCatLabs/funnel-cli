package main

import (
	"os"
	"path/filepath"
)

const appName = "wirecat-funnel"

// stateDir mirrors cli-base's resolvePaths, so Desktop's storage.rs finds this folder the way it finds the tools'.
func stateDir(getenv func(string) string, goos string) (string, error) {
	if dir := getenv("WIRECAT_FUNNEL_STATE_DIR"); dir != "" {
		return dir, nil
	}
	if goos == "windows" {
		local := getenv("LOCALAPPDATA")
		if local == "" {
			home, err := homeDir(getenv("USERPROFILE"))
			if err != nil {
				return "", err
			}
			local = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(local, appName, "Data"), nil
	}
	if data := getenv("XDG_DATA_HOME"); data != "" {
		return filepath.Join(data, appName), nil
	}
	home, err := homeDir(getenv("HOME"))
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", appName), nil
}

func homeDir(fromEnv string) (string, error) {
	if fromEnv != "" {
		return fromEnv, nil
	}
	return os.UserHomeDir()
}

func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

// writeFileAtomic keeps a reader from ever seeing half a file.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
