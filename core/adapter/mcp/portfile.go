package mcp

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/runtimepaths"
)

// WriteAPIPort records the listen port for Work's ResolveDanmoNovelMCPURL
// (~/.danmo-novel/api.port), matching Danmo Make's convention.
func WriteAPIPort(addr string) error {
	port := addr
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		port = addr[i+1:]
	}
	port = strings.TrimSpace(port)
	if port == "" {
		return nil
	}
	if _, err := strconv.Atoi(port); err != nil {
		return nil
	}
	if err := runtimepaths.EnsureDirs(); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(runtimepaths.Home(), "api.port"), []byte(port+"\n"), 0o644)
}
