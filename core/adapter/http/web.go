package httpapi

import (
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

func mountWeb(r *gin.Engine, assets fs.FS) {
	if assets == nil {
		return
	}
	files := http.FileServer(http.FS(assets))
	handler := func(c *gin.Context) {
		name := strings.TrimPrefix(c.Request.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(assets, name); err != nil {
			c.Request.URL.Path = "/"
		}
		files.ServeHTTP(c.Writer, c.Request)
	}
	r.GET("/", handler)
	r.GET("/assets/*path", handler)
	r.GET("/favicon.ico", handler)
	r.NoRoute(func(c *gin.Context) {
		if c.Request.Method == http.MethodGet && !strings.HasPrefix(c.Request.URL.Path, "/api/") && !strings.HasPrefix(c.Request.URL.Path, "/mcp") {
			handler(c)
			return
		}
		c.JSON(404, gin.H{"error": "not found"})
	})
}

// WebAssets returns the production frontend when built, nil in a source-only
// checkout. Runtime injection keeps `go test ./...` independent of npm.
func WebAssets() fs.FS {
	root := os.Getenv("NOVEL_WEB_DIST")
	if root == "" {
		return nil
	}
	assets, err := fs.Sub(os.DirFS(root), ".")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(assets, "index.html"); err != nil {
		return nil
	}
	return assets
}
