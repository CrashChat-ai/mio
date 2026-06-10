package webembed

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// distFS contains a committed placeholder for local Go builds. The Docker build
// replaces dist/ with ui/web/app/dist after the Vite build has completed.
//
//go:embed dist
var distFS embed.FS

func Handler() http.Handler {
	dist, err := fs.Sub(distFS, "dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "mio-web assets unavailable", http.StatusServiceUnavailable)
		})
	}

	files := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleaned := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if cleaned == "." || cleaned == "" {
			serveIndex(w, dist)
			return
		}

		if _, err := fs.Stat(dist, cleaned); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				serveIndex(w, dist)
				return
			}
			http.Error(w, "mio-web asset error", http.StatusInternalServerError)
			return
		}

		files.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, dist fs.FS) {
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		http.Error(w, "mio-web index unavailable", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(index)
}
