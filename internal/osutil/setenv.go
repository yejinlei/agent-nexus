package osutil

import (
	"os"
	"os/exec"
	"runtime"
)

// SetEnvPersist sets the given env var in the current process and persists
// it system-wide so that child processes (like codex CLI) can read it.
//
// - Windows: setx (requires elevated for machine-level, user-level by default)
// - macOS:  launchctl setenv
// - Linux:  echo export into shell rc (best-effort), plus set current process
func SetEnvPersist(key, val string) error {
	// Always set in current process too
	os.Setenv(key, val)

	switch runtime.GOOS {
	case "windows":
		// setx sets user-level persistent env var; visible to all new shells
		cmd := exec.Command("setx", key, val)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()

	case "darwin":
		// launchctl setenv affects new processes on macOS
		cmd := exec.Command("launchctl", "setenv", key, val)
		return cmd.Run()

	case "linux":
		// Best-effort: append to common shell rc files
		home, _ := os.UserHomeDir()
		for _, rc := range []string{".bashrc", ".zshrc", ".profile"} {
			fpath := home + "/" + rc
			if _, err := os.Stat(fpath); err == nil {
				cmd := exec.Command("sh", "-c", "echo 'export "+key+"=\""+val+"\"' >> "+fpath)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err == nil {
					return nil
				}
			}
		}
		return nil
	}

	return nil
}
