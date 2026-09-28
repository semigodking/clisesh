package kiro

import (
	"fmt"
	"os/exec"
)

// lookKiro returns the absolute path to the kiro-cli binary, or an error if
// it is not found on PATH.
func lookKiro() (string, error) {
	bin, err := exec.LookPath("kiro-cli")
	if err != nil {
		return "", fmt.Errorf("kiro-cli not found on PATH: %w\n\nPlease ensure 'kiro-cli' is installed and in your PATH.", err)
	}
	return bin, nil
}
