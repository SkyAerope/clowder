package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/SkyAerope/clowder/internal/app"
)

// sessions 内存 session 池（admin 登录）。
var sessions = struct {
	sync.Mutex
	m map[string]time.Time
}{m: map[string]time.Time{}}

const sessionCookie = "clowder_session"
const sessionTTL = 24 * time.Hour

func newSession() string {
	b := make([]byte, 16)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	sessions.Lock()
	sessions.m[tok] = time.Now().Add(sessionTTL)
	sessions.Unlock()
	return tok
}

func validSession(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	sessions.Lock()
	defer sessions.Unlock()
	exp, ok := sessions.m[c.Value]
	if !ok || time.Now().After(exp) {
		delete(sessions.m, c.Value)
		return false
	}
	return true
}

func publicAccess(a *app.App) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !a.Cfg.Dashboard.Public {
				if !a.Cfg.Dashboard.Admin || !validSession(r) {
					jsonErr(w, http.StatusUnauthorized, "login required")
					return
				}
			}
			next(w, r)
		}
	}
}

func adminAccess(a *app.App) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !a.Cfg.Dashboard.Admin {
				jsonErr(w, http.StatusForbidden, "admin panel disabled")
				return
			}
			if !validSession(r) {
				jsonErr(w, http.StatusUnauthorized, "login required")
				return
			}
			next(w, r)
		}
	}
}

func registerSessionAPI(mux *http.ServeMux, a *app.App) {
	mux.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			AdminKey string `json:"admin_key"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if !a.Cfg.Dashboard.Admin ||
			subtle.ConstantTimeCompare([]byte(body.AdminKey), []byte(a.Cfg.Dashboard.AdminKey)) != 1 {
			jsonErr(w, http.StatusUnauthorized, "invalid admin key")
			return
		}
		tok := newSession()
		http.SetCookie(w, &http.Cookie{
			Name: sessionCookie, Value: tok, Path: "/",
			HttpOnly: true, SameSite: http.SameSiteLaxMode,
			MaxAge: int(sessionTTL.Seconds()),
		})
		jsonOK(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /api/logout", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil {
			sessions.Lock()
			delete(sessions.m, c.Value)
			sessions.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
		jsonOK(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, map[string]any{
			"admin":  a.Cfg.Dashboard.Admin && validSession(r),
			"public": a.Cfg.Dashboard.Public,
		})
	})
}
