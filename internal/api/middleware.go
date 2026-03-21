package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	adminCookieName   = "admin_session"
	adminCookieSecret = "squares-admin-secret"
	adminCookieMaxAge = 7 * 24 * 60 * 60 // 7 days
)

func adminSessionValue(token string) string {
	mac := hmac.New(sha256.New, []byte(adminCookieSecret))
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

func AdminAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := os.Getenv("ADMIN_TOKEN")
		if token == "" {
			// Dev mode: no token set, allow all access
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie(adminCookieName)
		if err != nil || cookie.Value != adminSessionValue(token) {
			http.Redirect(w, r, "/admin/login", http.StatusFound)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		elapsed := time.Since(start)

		// Always log all requests
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rw.status, elapsed)

		// Log visitor details for page views (skip static assets, SSE, API endpoints)
		if r.Method == http.MethodGet &&
			!strings.HasPrefix(r.URL.Path, "/static/") &&
			!strings.HasSuffix(r.URL.Path, "/events") &&
			!strings.HasSuffix(r.URL.Path, "/grid") &&
			!strings.HasSuffix(r.URL.Path, "/leaderboard") &&
			!strings.HasSuffix(r.URL.Path, "/games") {
			ip := r.Header.Get("X-Forwarded-For")
			if ip == "" {
				ip = r.RemoteAddr
			}
			ua := r.Header.Get("User-Agent")
			ref := r.Header.Get("Referer")
			log.Printf("VISIT path=%s status=%d ip=%s ua=%q ref=%q", r.URL.Path, rw.status, ip, ua, ref)
		}
	})
}

func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("panic: %v", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
