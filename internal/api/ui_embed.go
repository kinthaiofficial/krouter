package api

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/kinthaiofficial/krouter/internal/webui"
)

// mountUI registers /ui/* routes with SPA fallback onto mux.
func mountUI(mux *http.ServeMux) {
	sub, err := fs.Sub(webui.Assets, "dist")
	if err != nil {
		return
	}
	fileServer := http.FileServer(http.FS(sub))
	mux.Handle("/ui/", http.StripPrefix("/ui", spaHandler(fileServer, sub)))
}

// spaHandler wraps a file server: requests for missing files return index.html.
//
// Note: we must NOT set r.URL.Path = "/index.html" and pass to FileServer —
// Go's FileServer redirects any path ending in "/index.html" to "./" (canonical
// URL stripping), which breaks deep-linked SPA routes. Read and write directly.
func spaHandler(fileServer http.Handler, fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fsys.Open(p); err != nil {
			data, err2 := fs.ReadFile(fsys, "index.html")
			if err2 != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(data) //nolint:errcheck
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
