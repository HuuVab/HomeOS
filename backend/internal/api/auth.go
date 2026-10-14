package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/auth"
	"github.com/HuuVab/HomeOS/backend/internal/redisaccel"
	"github.com/HuuVab/HomeOS/backend/internal/store"
)

const sessionCookie = "homeos_session"

type loginAttempt struct {
	count   int
	resetAt time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
}

func (l *loginLimiter) allow(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.attempts) > 1024 {
		for key, attempt := range l.attempts {
			if now.After(attempt.resetAt) {
				delete(l.attempts, key)
			}
		}
	}
	attempt := l.attempts[host]
	if now.After(attempt.resetAt) {
		attempt = loginAttempt{resetAt: now.Add(time.Minute)}
	}
	if attempt.count >= 10 {
		return false
	}
	attempt.count++
	l.attempts[host] = attempt
	return true
}

func registerAuthRoutes(mux *http.ServeMux, db *store.Store, accelerator *redisaccel.Layer) {
	limiter := &loginLimiter{attempts: make(map[string]loginAttempt)}
	mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		allowed := false
		if accelerator != nil {
			var err error
			allowed, err = accelerator.AllowLogin(r.Context(), r.RemoteAddr)
			if err != nil {
				log.Printf("Redis login limiter unavailable: %v", err)
				allowed = limiter.allow(r.RemoteAddr)
			}
		} else {
			allowed = limiter.allow(r.RemoteAddr)
		}
		if !allowed {
			writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many login attempts")
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			writeError(w, http.StatusBadRequest, "invalid_request", "Expected JSON")
			return
		}
		var credentials struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&credentials); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid login request")
			return
		}
		user, err := db.UserByUsername(credentials.Username)
		if errors.Is(err, sql.ErrNoRows) {
			// Spend comparable hashing work for unknown accounts.
			_, _ = auth.HashPassword("unknown-account")
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid username or password")
			return
		}
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "not_ready", "Service is not ready")
			return
		}
		if !auth.VerifyPassword(user.PasswordHash, credentials.Password) {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid username or password")
			return
		}
		token, tokenErr := auth.RandomToken()
		csrfToken, csrfErr := auth.RandomToken()
		if tokenErr != nil || csrfErr != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create session")
			return
		}
		if err := db.CreateSession(user.ID, tokenHash(token), csrfToken, time.Now().Add(24*time.Hour)); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create session")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil, MaxAge: 24 * 60 * 60})
		writeJSON(w, http.StatusOK, sessionResponse(user, csrfToken))
	})
	mux.HandleFunc("GET /api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		user, csrfToken, _, ok := authenticate(w, r, db)
		if ok {
			writeJSON(w, http.StatusOK, sessionResponse(user, csrfToken))
		}
	})
	mux.HandleFunc("POST /api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		_, csrfToken, hash, ok := authenticate(w, r, db)
		if !ok {
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(csrfToken)) != 1 {
			writeError(w, http.StatusForbidden, "invalid_csrf", "Invalid CSRF token")
			return
		}
		if err := db.RevokeSession(hash); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to sign out")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil, MaxAge: -1})
		w.WriteHeader(http.StatusNoContent)
	})
}

func sessionResponse(user store.User, csrfToken string) map[string]any {
	name := strings.TrimSpace(os.Getenv("HOMEOS_SERVER_NAME"))
	if name == "" {
		name, _ = os.Hostname()
	}
	if name == "" {
		name = "HomeOS server"
	}
	return map[string]any{
		"user": user, "csrfToken": csrfToken,
		"server": map[string]string{"name": name},
	}
}

func authenticate(w http.ResponseWriter, r *http.Request, db *store.Store) (store.User, string, string, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || len(cookie.Value) != 43 {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required")
		return store.User{}, "", "", false
	}
	hash := tokenHash(cookie.Value)
	user, csrfToken, err := db.Session(hash)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required")
		return store.User{}, "", "", false
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "Service is not ready")
		return store.User{}, "", "", false
	}
	return user, csrfToken, hash, true
}

func tokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
