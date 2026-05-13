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
func spaHandler(fileServer http.Handler, fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fsys.Open(p); err != nil {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/index.html"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
