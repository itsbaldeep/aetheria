package screens

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newGallery stands up a temp gallery dir with one sha set and returns a
// server + the token. cleanup via t.Cleanup.
func newGallery(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	token := "sekrit-token-123"
	sha := "ac9c4cb"
	imgDir := filepath.Join(dir, sha)
	thumbDir := filepath.Join(imgDir, "thumb")
	if err := os.MkdirAll(thumbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(imgDir, "01_login.png"), []byte("PNGDATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(thumbDir, "01_login.webp"), []byte("WEBPDATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, token
}

func do(t *testing.T, h http.Handler, method, target string, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: cookieName, Value: cookie})
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// galleryMux builds a ServeMux with screens.Mount for the given dir/token.
func galleryMux(dir, token string) http.Handler {
	mux := http.NewServeMux()
	Mount(mux, nil, dir, token)
	return mux
}

// --- token hygiene -------------------------------------------------------

// TestTokenQueryExchangesCookie verifies a valid ?t sets an HttpOnly cookie
// and 303-redirects to the clean URL (no ?t), preserving other query params.
func TestTokenQueryExchangesCookie(t *testing.T) {
	dir, token := newGallery(t)
	h := galleryMux(dir, token)

	req := httptest.NewRequest("GET", "/screens/ac9c4cb?compare=deadbee&t="+token, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("t") != "" {
		t.Errorf("redirect URL still contains t: %s", loc)
	}
	if u.Query().Get("compare") != "deadbee" {
		t.Errorf("compare param lost in redirect: %s", loc)
	}
	if u.Path != "/screens/ac9c4cb" {
		t.Errorf("redirect path changed: %s", loc)
	}
	var setCookie string
	for _, c := range rr.Result().Cookies() {
		if c.Name == cookieName {
			setCookie = c.Value
		}
	}
	if setCookie != token {
		t.Errorf("cookie not set to token; got %q", setCookie)
	}
	// HttpOnly must be true so JS can't read the token.
	raw := rr.Header().Get("Set-Cookie")
	if !strings.Contains(strings.ToLower(raw), "httponly") {
		t.Errorf("cookie not HttpOnly: %s", raw)
	}
}

// TestCookieAuthServes confirms the cookie alone (no ?t) authenticates.
func TestCookieAuthServes(t *testing.T) {
	dir, token := newGallery(t)
	h := galleryMux(dir, token)

	rr := do(t, h, "GET", "/screens", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("cookie auth expected 200, got %d", rr.Code)
	}
}

// TestTokenNotInLinks verifies rendered HTML links never carry ?t=<token>.
func TestTokenNotInLinks(t *testing.T) {
	dir, token := newGallery(t)
	h := galleryMux(dir, token)

	// index page
	rr := do(t, h, "GET", "/screens", token)
	body := rr.Body.String()
	if strings.Contains(body, "t="+token) {
		t.Errorf("index links leak token: %s", body)
	}
	// sha page
	rr = do(t, h, "GET", "/screens/ac9c4cb", token)
	body = rr.Body.String()
	if strings.Contains(body, "t="+token) {
		t.Errorf("sha page links leak token: %s", body)
	}
}

// TestNoTokenReturns404 confirms the gallery is disabled when token is empty.
func TestNoTokenReturns404(t *testing.T) {
	dir, _ := newGallery(t)
	h := galleryMux(dir, "")
	rr := do(t, h, "GET", "/screens", "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("empty token expected 404, got %d", rr.Code)
	}
}

// TestWrongTokenReturns404 confirms a wrong token/cookie is rejected.
func TestWrongTokenReturns404(t *testing.T) {
	dir, token := newGallery(t)
	h := galleryMux(dir, token)
	rr := do(t, h, "GET", "/screens", "wrong-token")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("wrong cookie expected 404, got %d", rr.Code)
	}
}

// TestBearerHeaderAuth confirms Authorization: Bearer works.
func TestBearerHeaderAuth(t *testing.T) {
	dir, token := newGallery(t)
	mux := http.NewServeMux()
	Mount(mux, nil, dir, token)
	req := httptest.NewRequest("GET", "/screens", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("bearer auth expected 200, got %d", rr.Code)
	}
}

// --- sha path validation -------------------------------------------------

// TestShaTraversalRejected verifies non-hex sha segments (.., %2e, etc.) are
// rejected with 404 rather than escaping the gallery root.
func TestShaTraversalRejected(t *testing.T) {
	dir, token := newGallery(t)
	h := galleryMux(dir, token)
	for _, bad := range []string{"ZZZZZZZ", "abc", "g123456", "deadbeef!", "too-long-sha"} {
		rr := do(t, h, "GET", "/screens/"+bad, token)
		// 404 (regex reject) or a redirect (ServeMux path-cleaning) — either
		// way it must never serve content from outside the gallery root.
		if rr.Code == http.StatusOK {
			t.Errorf("sha %q unexpectedly served 200 (body=%s)", bad, rr.Body.String())
		}
	}
}

// TestValidShaServed confirms a real hex sha is accepted.
func TestValidShaServed(t *testing.T) {
	dir, token := newGallery(t)
	h := galleryMux(dir, token)
	rr := do(t, h, "GET", "/screens/ac9c4cb", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("valid sha expected 200, got %d", rr.Code)
	}
}

// TestImageServed confirms a PNG under a valid sha is served.
func TestImageServed(t *testing.T) {
	dir, token := newGallery(t)
	h := galleryMux(dir, token)
	rr := do(t, h, "GET", "/screens/ac9c4cb/01_login.png", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("image expected 200, got %d", rr.Code)
	}
	b, _ := io.ReadAll(rr.Body)
	if string(b) != "PNGDATA" {
		t.Errorf("image body mismatch: %q", b)
	}
}

// TestThumbServed confirms a webp thumb under thumb/ is served.
func TestThumbServed(t *testing.T) {
	dir, token := newGallery(t)
	h := galleryMux(dir, token)
	rr := do(t, h, "GET", "/screens/ac9c4cb/thumb/01_login.webp", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("thumb expected 200, got %d", rr.Code)
	}
}

// TestFileTraversalRejected verifies path traversal inside a valid sha dir
// (e.g. /screens/<sha>/../../etc) is rejected.
func TestFileTraversalRejected(t *testing.T) {
	dir, token := newGallery(t)
	h := galleryMux(dir, token)
	rr := do(t, h, "GET", "/screens/ac9c4cb/../../etc/passwd", token)
	// ServeMux cleans the path to /etc/passwd which won't match /screens/,
	// so it 404s; either way it must not serve a system file.
	if rr.Code == http.StatusOK {
		t.Errorf("traversal unexpectedly served 200: %s", rr.Body.String())
	}
}

// --- reflected XSS -------------------------------------------------------

// TestShaEscapedInHTML verifies a sha (even if it reached render via a path
// that bypasses the regex) is HTML-escaped in the page, not echoed raw.
// We call renderShaPage directly with a crafted sha containing HTML.
func TestShaEscapedInHTML(t *testing.T) {
	dir, token := newGallery(t)
	h := &handler{dir: dir, token: token}
	// Build a sha dir whose name contains an XSS payload to simulate a
	// crafted directory entry that listShas would surface.
	xssSha := `<script>alert(1)</script>`
	xssDir := filepath.Join(dir, xssSha)
	if err := os.MkdirAll(xssDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xssDir, "01.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// listShas would include the xss sha; the index must escape it.
	req := httptest.NewRequest("GET", "/screens", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	rr := httptest.NewRecorder()
	h.renderIndex(rr, req)
	body := rr.Body.String()
	if strings.Contains(body, xssSha) {
		t.Errorf("index echoed unescaped XSS payload:\n%s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("index did not escape the payload (expected &lt;script&gt;):\n%s", body)
	}
}

// TestCompareEscapedInHTML verifies the compare query value is escaped.
func TestCompareEscapedInHTML(t *testing.T) {
	dir, token := newGallery(t)
	h := &handler{dir: dir, token: token}
	// renderShaPage validates compare against shaRe, so a non-hex compare
	// is dropped (empty). Verify a hex-looking compare that contains no XSS
	// is fine, and that an XSS compare is dropped entirely.
	req := httptest.NewRequest("GET", "/screens/ac9c4cb?compare=<script>x</script>", nil)
	rr := httptest.NewRecorder()
	h.renderShaPage(rr, req, "ac9c4cb", filepath.Join(dir, "ac9c4cb"))
	body := rr.Body.String()
	if strings.Contains(body, "<script>") {
		t.Errorf("compare XSS leaked into page:\n%s", body)
	}
}
