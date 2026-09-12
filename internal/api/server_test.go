package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"particeps/internal/auth"
	"particeps/internal/config"
	"particeps/internal/core"
	"particeps/internal/store"
)

func testServer(t *testing.T) (*Server, string) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	a := &auth.Auth{S: s}
	session, err := a.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	return &Server{App: &core.App{Store: s, Auth: a, Cfg: config.Defaults()}}, session
}

func tokenRequest(handler http.Handler, session, body, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "http://agent.test/api/v1/tokens", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if session != "" {
		r.AddCookie(&http.Cookie{Name: "particeps_session", Value: session})
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestTokenMutationRejectsInvalidJSON(t *testing.T) {
	for _, body := range []string{`{`, `null`, `{"name":"test","role":"read"} {}`, `{"name":"test","role":"read","rol":"manage"}`} {
		s, session := testServer(t)
		w := tokenRequest(s.Handler(), session, body, "http://agent.test")
		if w.Code != 400 {
			t.Errorf("malformed request status=%d", w.Code)
		}
		var count int
		if err := s.App.Store.DB.QueryRow(`SELECT COUNT(*) FROM tokens`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("malformed request created %d tokens", count)
		}
	}
}

func TestCookieMutationRequiresSameOrigin(t *testing.T) {
	s, session := testServer(t)
	for _, origin := range []string{"http://other.test", "null", "http://agent.test.evil.test"} {
		w := tokenRequest(s.Handler(), session, `{"name":"test","role":"read"}`, origin)
		if w.Code != 403 {
			t.Errorf("origin %q status=%d", origin, w.Code)
		}
	}
	w := tokenRequest(s.Handler(), session, `{"name":"test","role":"read"}`, "http://agent.test")
	if w.Code != 200 {
		t.Fatalf("same-origin request: %d %s", w.Code, w.Body.String())
	}
}

func TestReadTokenCannotMutateAndRevocationIsImmediate(t *testing.T) {
	s, _ := testServer(t)
	id, token, err := s.App.TokenCreate("test", "read")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/tokens", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("read token write status=%d", w.Code)
	}
	if err := s.App.TokenRevoke(id); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("revoked token status=%d", w.Code)
	}
}

func TestPortMutationsRequireManagePermissionAndSameOrigin(t *testing.T) {
	paths := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/instances/guest/ports"},
		{http.MethodPatch, "/api/v1/instances/guest/ports/20000/tcp"},
		{http.MethodPost, "/api/v1/instances/guest/ports/sync"},
	}
	for _, route := range paths {
		s, session := testServer(t)
		_, token, err := s.App.TokenCreate("reader", "read")
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range []string{"anonymous", "read-token", "cross-origin"} {
			r := httptest.NewRequest(route.method, "http://agent.test"+route.path, strings.NewReader(`{}`))
			want := http.StatusForbidden
			switch mode {
			case "anonymous":
				want = http.StatusUnauthorized
			case "read-token":
				r.Header.Set("Authorization", "Bearer "+token)
			case "cross-origin":
				r.AddCookie(&http.Cookie{Name: "particeps_session", Value: session})
				r.Header.Set("Origin", "http://other.test")
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != want {
				t.Errorf("%s %s: got %d, want %d", route.path, mode, w.Code, want)
			}
		}
	}
}
