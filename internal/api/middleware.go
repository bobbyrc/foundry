package api

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// contentSecurityPolicy allows only same-origin scripts, styles, fonts and connections. The
// built UI has no inline script or style, so it needs no 'unsafe-inline'. The favicon is a data
// URI, which img-src data: allows.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; " +
	"img-src 'self' data:; connect-src 'self'; font-src 'self'; object-src 'none'; " +
	"base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// securityHeaders sets browser hardening headers on every response, including errors and the
// web UI.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// noStore tells browsers never to keep an API response. Answers carry task data and sign-in
// state; a browser restoring tabs after a restart could otherwise replay a stored GET /auth/state
// from before the owner account existed and show the setup page again.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// hostOnly returns the lower-case host name of a Host header value or an allowlist entry: the
// port, IPv6 brackets and a trailing dot are removed.
func hostOnly(hostport string) string {
	h := strings.TrimSpace(hostport)
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	} else {
		h = strings.TrimPrefix(strings.TrimSuffix(h, "]"), "[")
	}
	return strings.ToLower(strings.TrimSuffix(h, "."))
}

// allowedHostSet returns the Host names the guard accepts: localhost, 127.0.0.1 and ::1 always,
// plus the extra names given (the bind host, the public URL host and server.allowed_hosts,
// computed by serve).
func allowedHostSet(extra []string) map[string]bool {
	set := map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}
	for _, h := range extra {
		if name := hostOnly(h); name != "" {
			set[name] = true
		}
	}
	return set
}

// hostGuard rejects requests whose Host name is not in allowed, which stops DNS rebinding: a
// page on another site cannot reach the API by pointing its own name at 127.0.0.1.
func hostGuard(allowed map[string]bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !allowed[hostOnly(r.Host)] {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusMisdirectedRequest)
				_, _ = fmt.Fprintln(w, "misdirected request: host not allowed")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

const (
	// maxListLimit caps ?limit= on list routes.
	maxListLimit = 1000
	// maxLogsLimit caps ?limit= on GET /runs/{id}/logs, which is read in larger pages.
	maxLogsLimit = 5000
)

// clampLimits lowers a ?limit= above the cap for its route to the cap. Values that are not
// integers, or are not above the cap, are left unchanged so handlers still reject bad input.
func clampLimits(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if raw := q.Get("limit"); raw != "" {
			limitCap := maxListLimit
			if isRunLogsPath(r.URL.Path) {
				limitCap = maxLogsLimit
			}
			if n, err := strconv.Atoi(raw); err == nil && n > limitCap {
				q.Set("limit", strconv.Itoa(limitCap))
				r.URL.RawQuery = q.Encode()
			}
		}
		next.ServeHTTP(w, r)
	})
}

// isRunLogsPath reports whether path is GET /api/v1/runs/{id}/logs.
func isRunLogsPath(path string) bool {
	rest, ok := strings.CutPrefix(path, "/api/v1/runs/")
	return ok && strings.HasSuffix(rest, "/logs") && strings.Count(rest, "/") == 1
}
