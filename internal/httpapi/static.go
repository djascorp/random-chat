package httpapi

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// spaHandler serves the built React bundle and falls back to index.html so
// client-side routes survive a hard refresh. When the bundle is absent the
// handler explains how to build it instead of returning a bare 404.
func (o Options) spaHandler() http.Handler {
	dir := o.Config.WebDir
	indexPath := filepath.Join(dir, "index.html")

	fileServer := http.FileServer(http.Dir(dir))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(indexPath); err != nil {
			o.writeMissingBundle(w)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/ws" {
			http.NotFound(w, r)
			return
		}

		cleaned := path.Clean(r.URL.Path)
		candidate := filepath.Join(dir, filepath.FromSlash(cleaned))
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			// Hashed assets are immutable, so let the browser cache them hard.
			if strings.HasPrefix(cleaned, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		// A missing file (favicon.ico, a stale asset hash) must 404. Falling back
		// to index.html there would answer a JS or image request with HTML.
		if path.Ext(cleaned) != "" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, indexPath)
	})
}

func (o Options) writeMissingBundle(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte("Le frontend n'est pas compilé.\n\n" +
		"  cd web && npm install && npm run build\n\n" +
		"puis relancez le serveur, ou utilisez le serveur de développement :\n\n" +
		"  cd web && npm run dev\n"))
}
