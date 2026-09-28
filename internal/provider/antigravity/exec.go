package antigravity

import (
	"fmt"
	"os/exec"
)

// lookAgy returns the absolute path to the agy binary, or an error if not found.
func lookAgy() (string, error) {
	bin, err := exec.LookPath("agy")
	if err != nil {
		return "", fmt.Errorf("agy (Antigravity CLI) not found on PATH: %w\n\nPlease ensure 'agy' is installed and in your PATH.", err)
	}
	return bin, nil
}
