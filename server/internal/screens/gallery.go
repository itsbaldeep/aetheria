// Package screens serves the M5.5 §1 screenshot review gallery from the
// admin server. The gallery root (AETHERIA_SCREEN_GALLERY, default
// /srv/screens) holds one directory per git short-sha, each containing
// NN_name.png captures + a thumb/ subdir of webp thumbnails + index.txt.
//
// Auth: until M8 lands full TOTP admin auth, the gallery is gated by a
// shared-secret bearer token (AETHERIA_SCREENS_TOKEN). Pass it as
// `Authorization: Bearer <token>`, via the aetheria_screens HttpOnly cookie,
// or once as `?t=<token>` (which sets the cookie and 303-redirects to a
// clean URL so the token never lingers in logs/history). Empty token = 404
// (gallery disabled). M8 will replace this gate with the real admin session.
package screens

import (
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/itsbaldeep/aetheria/server/internal/platform"
)

// cookieName is the HttpOnly cookie that carries the gallery token after the
// first ?t=<token> request exchanges it for a cookie.
const cookieName = "aetheria_screens"

// shaRe constrains the <sha> path segment to a real git hex sha (7–40 chars)
// so filepath.Join can never escape the gallery root via ".." or absolute paths.
var shaRe = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

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

// tokenFromCookie extracts the bearer token from the HttpOnly cookie, if any.
func (h *handler) tokenFromCookie(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// authed reports whether the request presents the gallery token via header
// or cookie. The ?t query path is handled by gate (it only bootstraps the
// cookie); authed itself never consults ?t so handlers can't leak it.
func (h *handler) authed(r *http.Request) bool {
	if h.token == "" {
		return false
	}
	if b := r.Header.Get("Authorization"); strings.HasPrefix(b, "Bearer ") {
		if strings.TrimPrefix(b, "Bearer ") == h.token {
			return true
		}
	}
	if h.tokenFromCookie(r) == h.token {
		return true
	}
	return false
}

// exchangeToken is called when a valid ?t=<token> is present: it sets the
// HttpOnly cookie and 303-redirects to the same path with `t` stripped from
// the query (other params like `compare` are preserved). This keeps the token
// out of Caddy access logs, browser history, and referrers.
func (h *handler) exchangeToken(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    h.token,
		Path:     "/screens",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
	q := r.URL.Query()
	q.Del("t")
	u := *r.URL
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

// gate is the auth wrapper shared by every route. It performs the ?t → cookie
// exchange (303 redirect) before serving, so handlers never echo the token.
func (h *handler) gate(w http.ResponseWriter, r *http.Request, serve func(http.ResponseWriter, *http.Request)) {
	if h.token == "" {
		http.NotFound(w, r)
		return
	}
	// Bootstrap path: a valid ?t trades itself for a cookie + clean redirect.
	if r.URL.Query().Get("t") == h.token {
		h.exchangeToken(w, r)
		return
	}
	if !h.authed(r) {
		http.NotFound(w, r)
		return
	}
	serve(w, r)
}

// index serves /screens — the list of published sha sets.
func (h *handler) index(w http.ResponseWriter, r *http.Request) {
	h.gate(w, r, h.renderIndex)
}

func (h *handler) renderIndex(w http.ResponseWriter, r *http.Request) {
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
			// Links rely on the HttpOnly cookie; no token in URL.
			fmtf(&b, `<div class="sha"><a href="/screens/%s">%s</a></div>`,
				html.EscapeString(s), html.EscapeString(s))
		}
	}
	b.WriteString("</body></html>")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(b.String()))
}

// route dispatches under /screens/:
//
//	/screens/                       -> index
//	/screens/<sha>/thumb/<f>.webp   -> thumb
//	/screens/<sha>/<f>.png          -> image
//	/screens/<sha>                  -> sha gallery page
func (h *handler) route(w http.ResponseWriter, r *http.Request) {
	h.gate(w, r, func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, "/screens/")
		rel = strings.Trim(rel, "/")
		if rel == "" {
			h.renderIndex(w, r)
			return
		}
		parts := strings.SplitN(rel, "/", 2)
		sha := parts[0]
		if !shaRe.MatchString(sha) {
			http.NotFound(w, r)
			return
		}
		shaDir := filepath.Join(h.dir, sha)
		if len(parts) == 1 {
			h.renderShaPage(w, r, sha, shaDir)
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
	})
}

func (h *handler) renderShaPage(w http.ResponseWriter, r *http.Request, sha, shaDir string) {
	compare := r.URL.Query().Get("compare")
	// Defense-in-depth: validate compare the same way as the path sha.
	if compare != "" && !shaRe.MatchString(compare) {
		compare = ""
	}
	escSha := html.EscapeString(sha)
	imgs := h.listImages(shaDir)
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=utf-8><title>")
	b.WriteString("Aetheria screens — ")
	b.WriteString(escSha)
	b.WriteString("</title>")
	b.WriteString(`<style>body{font-family:system-ui,sans-serif;background:#1a1230;color:#f3ecff;margin:24px}a{color:#5fd4e8}h1,h2{color:#d9a441}.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(320px,1fr));gap:12px}.shot{background:#241a3f;padding:8px;border-radius:6px}.shot img{width:100%;border-radius:4px}.shot a{color:#9d92b8;font-size:12px}.compare{display:grid;grid-template-columns:1fr 1fr;gap:16px}</style>`)
	b.WriteString("</head><body><h1>")
	b.WriteString("Screens — ")
	b.WriteString(escSha)
	b.WriteString(`</h1><p><a href="/screens">← all shas</a></p>`)
	if compare != "" {
		cImgs := h.listImages(filepath.Join(h.dir, compare))
		fmtf(&b, `<h2>Compare: %s vs %s</h2><div class="compare"><div>`,
			escSha, html.EscapeString(compare))
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
	escSha := html.EscapeString(sha)
	for _, name := range imgs {
		escName := html.EscapeString(name)
		thumb := "thumb/" + strings.TrimSuffix(name, ".png") + ".webp"
		escThumb := html.EscapeString(thumb)
		fmtf(b, `<div class="shot"><a href="/screens/%s/%s"><img src="/screens/%s/%s" alt="%s"></a><br><a href="/screens/%s/%s">%s</a></div>`,
			escSha, escName, escSha, escThumb, escName, escSha, escName, escName)
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
