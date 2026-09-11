package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"particeps/internal/store"
)

const cookieName = "particeps_session"
const sessionTTL = 12 * time.Hour

type Auth struct {
	S *store.Store
}

func (a *Auth) HasAdmin() bool {
	var n int
	_ = a.S.DB.QueryRow(`SELECT COUNT(*) FROM admin`).Scan(&n)
	return n > 0
}

func (a *Auth) SetAdminPassword(plain string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = a.S.DB.Exec(`INSERT INTO admin(id, password_hash) VALUES(1, ?)
		ON CONFLICT(id) DO UPDATE SET password_hash=excluded.password_hash`, hash)
	return err
}

func (a *Auth) CheckAdmin(plain string) bool {
	var hash []byte
	if err := a.S.DB.QueryRow(`SELECT password_hash FROM admin WHERE id=1`).Scan(&hash); err != nil {
		return false
	}
	return bcrypt.CompareHashAndPassword(hash, []byte(plain)) == nil
}

func (a *Auth) CreateSession() (string, error) {
	id := store.NewID() + store.NewID()
	now := time.Now()
	_, err := a.S.DB.Exec(`INSERT INTO sessions(id, created_at, expires_at) VALUES(?,?,?)`,
		id, now.Unix(), now.Add(sessionTTL).Unix())
	return id, err
}

func (a *Auth) ValidSession(id string) bool {
	if id == "" {
		return false
	}
	var exp int64
	err := a.S.DB.QueryRow(`SELECT expires_at FROM sessions WHERE id=?`, id).Scan(&exp)
	return err == nil && exp > time.Now().Unix()
}

func (a *Auth) DeleteSession(id string) {
	_, _ = a.S.DB.Exec(`DELETE FROM sessions WHERE id=?`, id)
}

func HashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

func NewTokenPlain() string {
	var b [24]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

type Principal struct {
	Role    string // admin | read | manage
	Via     string // session | token
	TokenID string
}

func (p Principal) CanWrite() bool {
	return p.Role == "admin" || p.Role == "manage"
}

func (a *Auth) Principal(r *http.Request) (Principal, bool) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(h), "bearer ") {
		plain := strings.TrimSpace(h[7:])
		var id, role string
		var revoked int
		err := a.S.DB.QueryRow(`SELECT id, role, revoked FROM tokens WHERE hash=?`, HashToken(plain)).
			Scan(&id, &role, &revoked)
		if err == nil && revoked == 0 {
			return Principal{Role: role, Via: "token", TokenID: id}, true
		}
		return Principal{}, false
	}
	c, err := r.Cookie(cookieName)
	if err != nil || !a.ValidSession(c.Value) {
		return Principal{}, false
	}
	return Principal{Role: "admin", Via: "session"}, true
}

func SetSessionCookie(w http.ResponseWriter, id string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   secure,
		Expires:  time.Now().Add(sessionTTL),
	})
}

func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}
