package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"particeps/internal/auth"
	"particeps/internal/core"
	"particeps/internal/procfs"
	"particeps/internal/web"
)

type Server struct {
	App *core.App
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/session", s.login)
	mux.HandleFunc("DELETE /api/v1/session", s.logout)
	mux.HandleFunc("GET /api/v1/meta", s.auth(false, s.meta))
	mux.HandleFunc("GET /api/v1/host", s.auth(false, s.host))
	mux.HandleFunc("GET /api/v1/host/network", s.auth(false, s.detected))
	mux.HandleFunc("GET /api/v1/settings/network-pool", s.auth(false, s.getPool))
	mux.HandleFunc("PUT /api/v1/settings/network-pool", s.auth(true, s.putPool))
	mux.HandleFunc("PUT /api/v1/settings/cpu-cap", s.auth(true, s.putCap))
	mux.HandleFunc("GET /api/v1/instances", s.auth(false, s.listInst))
	mux.HandleFunc("POST /api/v1/instances", s.auth(true, s.createInst))
	mux.HandleFunc("GET /api/v1/instances/{id}", s.auth(false, s.getInst))
	mux.HandleFunc("POST /api/v1/instances/{id}/start", s.auth(true, s.power("start", false)))
	mux.HandleFunc("POST /api/v1/instances/{id}/stop", s.auth(true, s.power("stop", false)))
	mux.HandleFunc("POST /api/v1/instances/{id}/force-stop", s.auth(true, s.power("stop", true)))
	mux.HandleFunc("POST /api/v1/instances/{id}/restart", s.auth(true, s.power("restart", false)))
	mux.HandleFunc("POST /api/v1/instances/{id}/delete", s.auth(true, s.delInst))
	mux.HandleFunc("POST /api/v1/instances/{id}/rebuild", s.auth(true, s.rebuild))
	mux.HandleFunc("PATCH /api/v1/instances/{id}/resources", s.auth(true, s.patchRes))
	mux.HandleFunc("POST /api/v1/instances/{id}/ports", s.auth(true, s.addPort))
	mux.HandleFunc("PATCH /api/v1/instances/{id}/ports/{number}/{proto}", s.auth(true, s.editPort))
	mux.HandleFunc("POST /api/v1/instances/{id}/ports/sync", s.auth(true, s.syncPorts))
	mux.HandleFunc("POST /api/v1/instances/{id}/password", s.auth(true, s.resetPW))
	mux.HandleFunc("GET /api/v1/instances/{id}/processes", s.auth(false, s.procs))
	mux.HandleFunc("GET /api/v1/instances/{id}/metrics", s.auth(false, s.metrics))
	mux.HandleFunc("GET /api/v1/tasks/{id}", s.auth(false, s.getTask))
	mux.HandleFunc("POST /api/v1/tasks/{id}/credentials", s.auth(true, s.claimCredentials))
	mux.HandleFunc("GET /api/v1/images", s.auth(false, s.images))
	mux.HandleFunc("POST /api/v1/images", s.auth(true, s.regImage))
	mux.HandleFunc("GET /api/v1/tokens", s.auth(true, s.tokens))
	mux.HandleFunc("POST /api/v1/tokens", s.auth(true, s.newToken))
	mux.HandleFunc("POST /api/v1/tokens/{id}/revoke", s.auth(true, s.revokeToken))
	mux.HandleFunc("GET /api/v1/metrics/host", s.auth(false, s.hostMetrics))
	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		dist = web.Dist
	}
	mux.Handle("/", spa(http.FS(dist)))
	return mux
}

func spa(fsys http.FileSystem) http.Handler {
	file := http.FileServer(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		f, err := fsys.Open(strings.TrimPrefix(r.URL.Path, "/"))
		if err != nil {
			r.URL.Path = "/"
		} else {
			_ = f.Close()
		}
		file.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *Server) auth(write bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.App.Auth.Principal(r)
		if !ok {
			writeErr(w, 401, "unauthorized")
			return
		}
		if write && !p.CanWrite() {
			writeErr(w, 403, "read-only token")
			return
		}
		if write && p.Via == "session" && !sameOrigin(r) {
			writeErr(w, http.StatusForbidden, "request origin is not allowed")
			return
		}
		next(w, r)
	}
}

func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" || site == "same-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	} // Non-browser API clients do not send Origin.
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && strings.EqualFold(u.Host, r.Host)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON request body")
		return false
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		writeErr(w, http.StatusBadRequest, "request body must be a JSON object")
		return false
	}
	objectDecoder := json.NewDecoder(bytes.NewReader(raw))
	objectDecoder.DisallowUnknownFields()
	if err := objectDecoder.Decode(dest); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON request fields")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, "request must contain one JSON object")
		return false
	}
	return true
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeErr(w, http.StatusForbidden, "request origin is not allowed")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !s.App.Auth.CheckAdmin(body.Password) {
		writeErr(w, 401, "invalid password")
		return
	}
	id, err := s.App.Auth.CreateSession()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	auth.SetSessionCookie(w, id, r.TLS != nil)
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeErr(w, http.StatusForbidden, "request origin is not allowed")
		return
	}
	if c, err := r.Cookie("particeps_session"); err == nil {
		s.App.Auth.DeleteSession(c.Value)
	}
	auth.ClearSessionCookie(w)
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

func (s *Server) meta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"product": "particeps", "api": "v1"})
}

func (s *Server) host(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.App.HostSnapshot())
}

func (s *Server) detected(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"addresses": s.App.DetectedNetwork()})
}

func (s *Server) getPool(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.App.Pool())
}

func (s *Server) putPool(w http.ResponseWriter, r *http.Request) {
	var p core.PoolSettings
	if !decodeJSON(w, r, &p) {
		return
	}
	if err := s.App.SetPool(p); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) putCap(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Cores float64 `json:"cores"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.App.SetCPUCap(body.Cores); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	cores, cap := s.App.CPUCapStatus()
	writeJSON(w, 200, map[string]any{"cores": cores, "cap": cap})
}

func (s *Server) listInst(w http.ResponseWriter, r *http.Request) {
	list, err := s.App.ListInstances()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"instances": list})
}

func (s *Server) createInst(w http.ResponseWriter, r *http.Request) {
	var req core.CreateReq
	if !decodeJSON(w, r, &req) {
		return
	}
	task, err := s.App.SubmitCreate(req, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeErr(w, 409, err.Error())
		return
	}
	writeJSON(w, 202, task)
}

func (s *Server) getInst(w http.ResponseWriter, r *http.Request) {
	in, ports, err := s.App.GetInstance(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	in.Ports = ports
	writeJSON(w, 200, in)
}

func (s *Server) power(action string, force bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := s.App.Power(r.PathValue("id"), action, force); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "1"})
	}
}

func (s *Server) delInst(w http.ResponseWriter, r *http.Request) {
	if err := s.App.DeleteInstance(r.PathValue("id")); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

func (s *Server) addPort(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Number int `json:"number"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.App.AddPort(r.PathValue("id"), body.Number); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	s.getInst(w, r)
}

func (s *Server) editPort(w http.ResponseWriter, r *http.Request) {
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid port number")
		return
	}
	var body struct {
		Target int `json:"target"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.App.EditPort(r.PathValue("id"), number, r.PathValue("proto"), body.Target); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	s.getInst(w, r)
}

func (s *Server) syncPorts(w http.ResponseWriter, r *http.Request) {
	if err := s.App.SyncPorts(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	s.getInst(w, r)
}

func (s *Server) rebuild(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Image string `json:"image"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.App.Rebuild(r.PathValue("id"), body.Image); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

func (s *Server) patchRes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CPUCores      *float64 `json:"cpuCores"`
		CPUPin        *string  `json:"cpuPin"`
		MemoryMiB     *int     `json:"memoryMib"`
		DiskGiB       *int     `json:"diskGib"`
		BandwidthMbps *int     `json:"bandwidthMbps"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.App.PatchResources(r.PathValue("id"), body.CPUCores, body.CPUPin, body.MemoryMiB, body.DiskGiB, body.BandwidthMbps); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

func (s *Server) resetPW(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	pw, err := s.App.ResetPassword(r.PathValue("id"), body.Password)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"password": pw})
}

func (s *Server) procs(w http.ResponseWriter, r *http.Request) {
	in, _, err := s.App.GetInstance(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	st, err := s.App.Incus.GetState(in.IncusName)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "instance process state is unavailable")
		return
	}
	processes := procfs.GuestProcs(st.Pid)
	if processes == nil {
		processes = []procfs.Proc{}
	}
	writeJSON(w, 200, map[string]any{"instanceId": in.ID, "processes": processes})
}

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	in, _, err := s.App.GetInstance(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	ser, err := s.App.Series(in.ID, r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"series": ser})
}

func (s *Server) hostMetrics(w http.ResponseWriter, r *http.Request) {
	ser, err := s.App.Series("host", r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"series": ser})
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	t, err := s.App.GetTask(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, t)
}

func (s *Server) claimCredentials(w http.ResponseWriter, r *http.Request) {
	credentials, err := s.App.ClaimInitialCredentials(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": credentials})
}

func (s *Server) images(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"images": s.App.Images()})
}

func (s *Server) regImage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Alias string `json:"alias"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.App.RegisterImage(body.Alias); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

func (s *Server) tokens(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"tokens": s.App.Tokens()})
}

func (s *Server) newToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	id, plain, err := s.App.TokenCreate(body.Name, body.Role)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"id": id, "token": plain, "role": body.Role})
}

func (s *Server) revokeToken(w http.ResponseWriter, r *http.Request) {
	if err := s.App.TokenRevoke(r.PathValue("id")); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"ok": "1"})
}
