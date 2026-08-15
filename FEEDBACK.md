# FEEDBACK — human playtest observations

The human writes playtest notes here (never overwritten by the agent).
The agent reads this at the start of each loop iteration, folds items into the
plan (human feedback outranks the default task order), and appends
`-> resolved:` notes under items it has addressed.

Expected human playtests: end of M2, M3, M5, M7, M10.

## M0 (no playtest expected — infrastructure milestone)
(empty)

---

## How to add feedback
Append a dated block: `## 2026-MM-DD — playtest <milestone>` followed by
`- ` bullet lines. A dated section containing unresolved `- ` bullets pauses
the autonomous loop until you address them.

## 2026-08-15 — screens review ac9c4cb (M5.5)
- Pipeline itself: APPROVED. Fix these three gallery issues before UI blocks proceed:
- gallery.go: the <sha> path segment is never validated — filepath.Join(h.dir, sha)
  with sha=".." escapes the gallery root (ServeMux path-cleaning mitigates, but add
  defense-in-depth: reject sha not matching ^[0-9a-f]{7,40}$).
  -> resolved: gallery.go now rejects any <sha> not matching ^[0-9a-f]{7,40}$ with
     404 before filepath.Join (shaRe). Covered by TestShaTraversalRejected.
- gallery.go: sha and compare query values are echoed into HTML unescaped (shaPage
  h1/h2) — reflected XSS post-auth. Wrap all echoed values in html.EscapeString.
  -> resolved: every echoed sha/compare/name/thumb value passes html.EscapeString;
     links no longer embed the token. Covered by TestShaEscapedInHTML +
     TestCompareEscapedInHTML.
- Token hygiene: ?t=<token> on every link/img puts the token in Caddy access logs
  and browser history. On first request with a valid ?t, set an HttpOnly cookie and
  303-redirect to the clean URL; accept the cookie thereafter, drop ?t from links.
  -> resolved: gate() performs the ?t→cookie exchange (HttpOnly, SameSite=Lax,
     Secure over TLS) + 303 to the clean URL preserving other params; authed()
     now only accepts header/cookie (never reads ?t for serving); all rendered
     links drop ?t. Covered by TestTokenQueryExchangesCookie + TestTokenNotInLinks
     + TestCookieAuthServes.
- Cosmetic, no rush: run_tour.sh TOUR_RC error branch is dead code under set -e.
  -> resolved: tour run now wraps `set +e` around timeout so TOUR_RC is captured
     and the error branch is live.
