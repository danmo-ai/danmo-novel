package pipeline

import (
	"os"
	"path/filepath"
)

func promptOverlayRoot() string {
	if root := os.Getenv("NOVEL_HOME"); root != "" {
		return filepath.Join(root, "prompts")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".danmo-novel", "prompts")
}
