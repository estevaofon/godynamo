package gui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// startElectron launches the dev Electron app in dir, passing the bridge port
// and token via environment variables (not argv, so the token is not visible in
// process listings). dir is resolved by the caller (see Run).
func startElectron(dir string, port int, token string) (*exec.Cmd, error) {
	bin := electronBinPath(dir)
	if _, statErr := os.Stat(bin); statErr != nil {
		if os.IsNotExist(statErr) {
			return nil, fmt.Errorf(
				"Electron is not set up. Run:\n  cd %s\n  npm install\nthen re-run `godynamo`", dir)
		}
		return nil, fmt.Errorf("checking Electron binary: %w", statErr)
	}

	cmd := exec.Command(bin, ".")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("GODYNAMO_BRIDGE_PORT=%d", port),
		"GODYNAMO_BRIDGE_TOKEN="+token,
	)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start Electron: %w", err)
	}
	return cmd, nil
}

// electronAppDir returns the path to the ./electron app folder, preferring a path
// next to the executable and falling back to the current working directory (dev).
func electronAppDir() (string, error) {
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "electron")
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, nil
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, "electron"), nil
}

// electronBinPath returns the dev Electron binary inside the app's node_modules.
func electronBinPath(electronDir string) string {
	base := filepath.Join(electronDir, "node_modules", ".bin")
	if runtime.GOOS == "windows" {
		return filepath.Join(base, "electron.cmd")
	}
	return filepath.Join(base, "electron")
}
