// Package frontend serves the React SPA bundle produced by
// `frontend/pnpm build`. The dist directory is embedded via
// //go:embed so a single hexplus binary still ships the whole panel.
//
// When dist/ is empty (fresh clone, before running `pnpm build`), the
// http.FileServer returns 404s for every asset — the Go panel still
// runs correctly, the user just sees a bare page. Documented as such
// in the README.
package frontend

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler returns an http.Handler that serves the SPA:
//   - static asset requests (foo.js, foo.css, favicon) get their file
//   - anything else (routes react-router owns) gets index.html
//
// urlPrefix is the panel's random path segment; rendered into the
// <base href="..."> tag of index.html so relative asset paths resolve
// correctly regardless of where the panel is mounted.
func Handler(urlPrefix string) http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "frontend bundle missing (run: cd frontend && pnpm build)", http.StatusServiceUnavailable)
		})
	}
	fileSrv := http.FileServer(http.FS(sub))
	basePath := urlPrefix
	if basePath == "" || basePath == "/" {
		basePath = ""
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve index.html for any path that doesn't correspond to an
		// on-disk file. This is the standard SPA fallback for a
		// react-router BrowserRouter.
		f, err := sub.Open(strings.TrimPrefix(r.URL.Path, "/"))
		if err == nil {
			_ = f.Close()
			fileSrv.ServeHTTP(w, r)
			return
		}
		if !errors.Is(err, fs.ErrNotExist) {
			// Filesystem error other than missing — surface it.
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		serveIndex(w, sub, basePath)
	})
}

// serveIndex reads index.html once and rewrites the <base href="..." />
// element to the current URL prefix, so a single bundle serves at any
// mount point.
func serveIndex(w http.ResponseWriter, sub fs.FS, basePath string) {
	raw, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		http.Error(w, "index.html missing", http.StatusServiceUnavailable)
		return
	}
	if basePath == "" {
		basePath = "/"
	} else {
		basePath = strings.TrimRight(basePath, "/") + "/"
	}
	// Naive substring rewrite is fine — the template ships with a
	// stable placeholder <base href="/" id="hexplus-base" />.
	patched := strings.Replace(string(raw),
		`<base href="/" id="hexplus-base" />`,
		`<base href="`+basePath+`" id="hexplus-base" />`,
		1,
	)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(patched))
}
