package api

import (
	"embed"
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

//go:embed all:webdist
var webFS embed.FS

func init() {
	// Go's built-in MIME table has no entry for .webmanifest, so http.FileServer would send it
	// as text/plain. The browser still reads it, but install prompts expect the manifest type.
	if err := mime.AddExtensionType(".webmanifest", "application/manifest+json"); err != nil {
		panic("api: mime: " + err.Error())
	}
}

// DefaultWeb returns the web UI embedded from webdist, the build output of web/.
func DefaultWeb() fs.FS {
	sub, err := fs.Sub(webFS, "webdist")
	if err != nil {
		// The directory is embedded at build time, so this cannot fail at run time.
		panic("api: webdist: " + err.Error())
	}
	return sub
}

const notBuiltPage = `<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>onett</title>
<p>onett is running. The web UI is not built: run <code>npm run build</code> in <code>web/</code>.</p>
`

// serveWeb serves a file from the UI, or index.html for any other path (the single-page app
// routes client-side). Without index.html it serves a short page saying the UI is not built.
func (s *server) serveWeb(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, r)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name != "" && name != "index.html" {
		if info, err := fs.Stat(s.web, name); err == nil && !info.IsDir() {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/" + name
			http.FileServer(http.FS(s.web)).ServeHTTP(w, r2)
			return
		}
	}
	body, err := fs.ReadFile(s.web, "index.html")
	if errors.Is(err, fs.ErrNotExist) {
		body, err = []byte(notBuiltPage), nil
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(body)
	}
}
