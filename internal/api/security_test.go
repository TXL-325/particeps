package api

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
	"particeps/internal/procfs"
)

func TestSessionCookieIsSecureBehindHTTPSProxy(t *testing.T) {
	for _, scenario := range []struct {
		name              string
		secure, tls, want bool
	}{
		{"proxy", true, false, true}, {"explicit-local-http", false, false, false}, {"direct-tls", false, true, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			s, _ := testServer(t)
			s.App.Cfg.SessionCookieSecure = scenario.secure
			if err := s.App.Auth.SetAdminPassword("fixture-admin-password"); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "http://agent.test/api/v1/session", strings.NewReader("{\"password\":\"fixture-admin-password\"}"))
			r.Header.Set("Origin", "https://agent.test")
			r.Header.Set("X-Forwarded-Proto", "http")
			if scenario.tls {
				r.TLS = &tls.ConnectionState{}
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			cookies := w.Result().Cookies()
			if w.Code != 200 || len(cookies) != 1 {
				t.Fatalf("login status=%d", w.Code)
			}
			if cookies[0].Secure != scenario.want || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
				t.Fatal("wrong cookie security policy")
			}
		})
	}
}
func TestLogoutFailsVisiblyUntilSessionIsRevoked(t *testing.T) {
	s, session := testServer(t)
	if _, err := s.App.Store.DB.Exec("CREATE TRIGGER fail_logout BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	logout := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("DELETE", "http://agent.test/api/v1/session", nil)
		r.AddCookie(&http.Cookie{Name: "particeps_session", Value: session})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	failed := logout()
	if failed.Code != 500 || len(failed.Result().Cookies()) != 0 || !s.App.Auth.ValidSession(session) {
		t.Fatal("failed logout pretends the session is revoked")
	}
	if _, err := s.App.Store.DB.Exec("DROP TRIGGER fail_logout"); err != nil {
		t.Fatal(err)
	}
	if logout().Code != 200 || s.App.Auth.ValidSession(session) {
		t.Fatal("successful logout did not revoke session")
	}
	r := httptest.NewRequest("GET", "http://agent.test/api/v1/meta", nil)
	r.AddCookie(&http.Cookie{Name: "particeps_session", Value: session})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("old session can be replayed")
	}
}
func TestLoginRateLimitUsesConnectionPeerAndReturnsRetryAfter(t *testing.T) {
	s, _ := testServer(t)
	if err := s.App.Auth.SetAdminPassword("fixture-admin-password"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		r := httptest.NewRequest("POST", "http://agent.test/api/v1/session", strings.NewReader("{\"password\":\"wrong-fixture-password\"}"))
		r.RemoteAddr = "192.0.2.20:" + strconv.Itoa(1000+i)
		r.Header.Set("X-Forwarded-For", "198.51.100."+strconv.Itoa(i))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		want := 401
		if i >= 5 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("attempt %d: %d want %d", i, w.Code, want)
		}
		if want == 429 {
			seconds, err := strconv.Atoi(w.Header().Get("Retry-After"))
			if err != nil || seconds < 1 {
				t.Fatal("missing retry interval")
			}
		}
	}
}
func TestAllRegisteredManagementRoutesRequireAuthorization(t *testing.T) {
	s, session := testServer(t)
	_, token, err := s.App.TokenCreate("reader", "read")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	routes := regexp.MustCompile("mux.HandleFunc\\(\"([A-Z]+) ([^\"]+)\"").FindAllStringSubmatch(string(source), -1)
	count := 0
	for _, route := range routes {
		method, path := route[1], route[2]
		if path == "/api/v1/session" {
			continue
		}
		count++
		path = strings.NewReplacer("{id}", "guest", "{number}", "20000", "{proto}", "tcp").Replace(path)
		modes := []string{"anonymous"}
		if method != "GET" || path == "/api/v1/tokens" {
			modes = append(modes, "reader", "cross-origin")
		}
		for _, mode := range modes {
			r := httptest.NewRequest(method, "http://agent.test"+path, strings.NewReader("{}"))
			want := 401
			if mode == "reader" {
				r.Header.Set("Authorization", "Bearer "+token)
				want = 403
			}
			if mode == "cross-origin" {
				r.AddCookie(&http.Cookie{Name: "particeps_session", Value: session})
				r.Header.Set("Origin", "https://untrusted.example")
				want = 403
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != want {
				t.Errorf("%s %s %s: got %d", method, path, mode, w.Code)
			}
		}
	}
	if count != 30 {
		t.Fatalf("review the authorization matrix for %d protected routes", count)
	}
}
func TestExpiredSessionAndInvalidBearerNeverFallBackToAdmin(t *testing.T) {
	s, session := testServer(t)
	for _, mode := range []string{"invalid-bearer", "expired-cookie"} {
		r := httptest.NewRequest("GET", "http://agent.test/api/v1/meta", nil)
		r.AddCookie(&http.Cookie{Name: "particeps_session", Value: session})
		if mode == "invalid-bearer" {
			r.Header.Set("Authorization", "Bearer invalid-fixture")
		} else {
			if _, err := s.App.Store.DB.Exec("UPDATE sessions SET expires_at=?", time.Now().Add(-time.Hour).Unix()); err != nil {
				t.Fatal(err)
			}
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 401 {
			t.Errorf("%s accepted", mode)
		}
	}
}
func TestTokenListingReportsStorageErrorsAndUsesEmptyArray(t *testing.T) {
	s, session := testServer(t)
	list := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "http://agent.test/api/v1/tokens", nil)
		r.AddCookie(&http.Cookie{Name: "particeps_session", Value: session})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	empty := list()
	var result struct{ Tokens []any }
	if err := json.Unmarshal(empty.Body.Bytes(), &result); err != nil || empty.Code != 200 || result.Tokens == nil {
		t.Fatal("empty list is not an array")
	}
	if _, err := s.App.Store.DB.Exec("DROP TABLE tokens"); err != nil {
		t.Fatal(err)
	}
	if list().Code != 500 {
		t.Fatal("storage failure reported as an empty list")
	}
}
func TestTokenCreateListAndRevokeThroughAPI(t *testing.T) {
	s, session := testServer(t)
	issue := tokenRequest(s.Handler(), session, "{\"name\":\"automation\",\"role\":\"read\"}", "http://agent.test")
	var token struct{ ID, Token string }
	if err := json.Unmarshal(issue.Body.Bytes(), &token); err != nil || issue.Code != 200 || token.Token == "" {
		t.Fatal("token issuance failed")
	}
	r := httptest.NewRequest("GET", "http://agent.test/api/v1/tokens", nil)
	r.AddCookie(&http.Cookie{Name: "particeps_session", Value: session})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), token.Token) || strings.Contains(w.Body.String(), "hash") {
		t.Fatal("token list exposes verifier or secret")
	}
	r = httptest.NewRequest("POST", "http://agent.test/api/v1/tokens/"+token.ID+"/revoke", nil)
	r.AddCookie(&http.Cookie{Name: "particeps_session", Value: session})
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("revocation failed")
	}
	r = httptest.NewRequest("GET", "http://agent.test/api/v1/meta", nil)
	r.Header.Set("Authorization", "Bearer "+token.Token)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("revoked token still authenticates")
	}
}

func TestOpenAPICoversRegisteredRoutesAndReferences(t *testing.T) {
	data, err := os.ReadFile("../../docs/api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	paths := doc["paths"].(map[string]any)
	if doc["openapi"] != "3.0.3" || doc["security"] == nil {
		t.Fatal("missing version or default authentication")
	}
	source, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	routes := regexp.MustCompile("mux.HandleFunc\\(\"([A-Z]+) (/api/v1[^\"]+)\"").FindAllStringSubmatch(string(source), -1)
	seen := map[string]bool{}
	ids := map[string]bool{}
	for _, route := range routes {
		method, path := strings.ToLower(route[1]), strings.TrimPrefix(route[2], "/api/v1")
		item, _ := paths[path].(map[string]any)
		op, _ := item[method].(map[string]any)
		if op == nil {
			t.Errorf("missing %s %s", method, path)
			continue
		}
		seen[method+" "+path] = true
		responses, _ := op["responses"].(map[string]any)
		if len(responses) == 0 {
			t.Errorf("%s %s has no responses", method, path)
		}
		id, _ := op["operationId"].(string)
		if id == "" || ids[id] {
			t.Errorf("missing or duplicate operationId %s", id)
		}
		ids[id] = true
		if path == "/session" {
			if op["security"] == nil {
				t.Fatal("public session operation inherits required bearer auth")
			}
		} else {
			if op["security"] != nil {
				t.Fatal("protected operation overrides authentication")
			}
			if op["x-permissions"] == nil {
				t.Fatal("role requirements missing")
			}
		}
	}
	for path, value := range paths {
		item := value.(map[string]any)
		for method := range item {
			if method != "parameters" && !seen[method+" "+path] {
				t.Errorf("documented route is not registered: %s %s", method, path)
			}
		}
	}
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if reference, ok := v["$ref"].(string); ok {
				var target any = doc
				for _, part := range strings.Split(strings.TrimPrefix(reference, "#/"), "/") {
					object, ok := target.(map[string]any)
					if !ok {
						target = nil
						break
					}
					target = object[part]
				}
				if target == nil {
					t.Errorf("broken reference %s", reference)
				}
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(doc)
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	process := schemas["Process"].(map[string]any)["properties"].(map[string]any)
	field, _ := reflect.TypeOf(procfs.Proc{}).FieldByName("Start")
	if field.Type.Kind() == reflect.Uint64 && process["start"].(map[string]any)["type"] != "integer" {
		t.Fatal("process start counter is documented as a nonnumeric field")
	}
	processResponses := paths["/instances/{id}/processes"].(map[string]any)["get"].(map[string]any)["responses"].(map[string]any)
	if processResponses["502"] == nil {
		t.Fatal("missing process backend failure response")
	}
}
