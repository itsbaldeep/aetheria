// Package screens serves the M5.5 §1 screenshot review gallery from the
// admin server. The gallery root (AETHERIA_SCREEN_GALLERY, default
// /srv/screens) holds one directory per git short-sha, each containing
// NN_name.png captures + a thumb/ subdir of webp thumbnails + index.txt.
//
// Auth: until M8 lands full TOTP admin auth, the gallery is gated by a
// shared-secret bearer token (AETHERIA_SCREENS_TOKEN). Pass it as
// `Authorization: Bearer <token>` or `?t=<token>`. Empty token = 404 (gallery
// disabled). M8 will replace this gate with the real admin session.
package screens

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/itsbaldeep/aetheria/server/internal/platform"
)

// Mount registers the gallery routes on mux. galleryDir is the host path the
// admin container sees (bind-mounted). token gates every request.
func Mount(mux *http.ServeMux, svc *platform.Service, galleryDir, token string) {
	if galleryDir == "" {
		galleryDir = "/srv/screens"
	}
	h := &handler{svc: svc, dir: galleryDir, token: token}
	mux.HandleFunc("/screens", h.index)
	mux.HandleFunc("/screens/", h.route)
}

type handler struct {
	svc   *platform.Service
	dir   string
	token string
}

func (h *handler) authed(r *http.Request) bool {
	if h.token == "" {
		return false
	}
	if b := r.Header.Get("Authorization"); strings.HasPrefix(b, "Bearer ") {
		if strings.TrimPrefix(b, "Bearer ") == h.token {
			return true
		}
	}
	if r.URL.Query().Get("t") == h.token {
		return true
	}
	return false
}

// route dispatches: /screens/            -> index
//
//	/screens/<sha>/thumb/<f>.webp -> thumb
//	/screens/<sha>/<f>.png        -> image
//	/screens/<sha>                -> sha gallery page
func (h *handler) route(w http.ResponseWriter, r *http.Request) {
	if !h.authed(r) {
		http.NotFound(w, r)
		return
	}
	rel := strings.TrimPrefix(r.URL.Path, "/screens/")
	rel = strings.Trim(rel, "/")
	if rel == "" {
		h.index(w, r)
		return
	}
	parts := strings.SplitN(rel, "/", 2)
	sha := parts[0]
	shaDir := filepath.Join(h.dir, sha)
	if len(parts) == 1 {
		h.shaPage(w, r, sha, shaDir)
		return
	}
	// serve a file (png or thumb/webp) — strict path, no traversal
	sub := parts[1]
	clean := filepath.Clean(sub)
	if strings.Contains(clean, "..") {
		http.NotFound(w, r)
		return
	}
	full := filepath.Join(shaDir, clean)
	if !strings.HasPrefix(full, shaDir) {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, full)
}

func (h *handler) index(w http.ResponseWriter, r *http.Request) {
	if !h.authed(r) {
		http.NotFound(w, r)
		return
	}
	shas := h.listShas()
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=utf-8><title>Aetheria screenshot gallery</title>")
	b.WriteString(`<style>body{font-family:system-ui,sans-serif;background:#1a1230;color:#f3ecff;margin:24px}a{color:#5fd4e8}h1{color:#d9a441}.sha{padding:8px 0}</style>`)
	b.WriteString("</head><body><h1>Aetheria — screenshot review</h1>")
	b.WriteString("<p>Each entry is one <code>make screenshots</code> run keyed by git short-sha. Click a sha to review the captures; pass <code>&amp;compare=&lt;sha&gt;</code> on a sha page for a side-by-side.</p>")
	if len(shas) == 0 {
		b.WriteString("<p><em>No screenshot sets published yet.</em></p>")
	} else {
		for _, s := range shas {
			fmtf(&b, `<div class="sha"><a href="/screens/%s?t=%s">%s</a></div>`, s, h.token, s)
		}
	}
	b.WriteString("</body></html>")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(b.String()))
}

func (h *handler) shaPage(w http.ResponseWriter, r *http.Request, sha, shaDir string) {
	compare := r.URL.Query().Get("compare")
	imgs := h.listImages(shaDir)
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=utf-8><title>")
	b.WriteString("Aetheria screens — ")
	b.WriteString(sha)
	b.WriteString("</title>")
	b.WriteString(`<style>body{font-family:system-ui,sans-serif;background:#1a1230;color:#f3ecff;margin:24px}a{color:#5fd4e8}h1,h2{color:#d9a441}.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(320px,1fr));gap:12px}.shot{background:#241a3f;padding:8px;border-radius:6px}.shot img{width:100%;border-radius:4px}.shot a{color:#9d92b8;font-size:12px}.compare{display:grid;grid-template-columns:1fr 1fr;gap:16px}</style>`)
	b.WriteString("</head><body><h1>")
	b.WriteString("Screens — ")
	b.WriteString(sha)
	b.WriteString("</h1><p><a href=\"/screens?t=")
	b.WriteString(h.token)
	b.WriteString("\">← all shas</a></p>")
	if compare != "" {
		cImgs := h.listImages(filepath.Join(h.dir, compare))
		fmtf(&b, `<h2>Compare: %s vs %s</h2><div class="compare"><div>`, sha, compare)
		h.thumbGrid(&b, sha, imgs)
		b.WriteString("</div><div>")
		h.thumbGrid(&b, compare, cImgs)
		b.WriteString("</div></div>")
	} else {
		b.WriteString(`<div class="grid">`)
		h.thumbGrid(&b, sha, imgs)
		b.WriteString("</div>")
	}
	b.WriteString("</body></html>")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(b.String()))
}

func (h *handler) thumbGrid(b *strings.Builder, sha string, imgs []string) {
	for _, name := range imgs {
		thumb := "thumb/" + strings.TrimSuffix(name, ".png") + ".webp"
		fmtf(b, `<div class="shot"><a href="/screens/%s/%s?t=%s"><img src="/screens/%s/%s?t=%s" alt="%s"></a><br><a href="/screens/%s/%s?t=%s">%s</a></div>`,
			sha, name, h.token, sha, thumb, h.token, name, sha, name, h.token, name)
	}
}

func (h *handler) listShas() []string {
	ents, err := os.ReadDir(h.dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out))) // newest sha-ish first
	return out
}

func (h *handler) listImages(shaDir string) []string {
	ents, err := os.ReadDir(shaDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".png") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func fmtf(b *strings.Builder, format string, args ...any) {
	b.WriteString(fmt.Sprintf(format, args...))
}
