package incusx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	http    *http.Client
	base    string
	project string
}

func Connect(socket, project string) *Client {
	t := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}
	return &Client{
		http:    &http.Client{Transport: t, Timeout: 15 * time.Minute},
		base:    "http://unix",
		project: project,
	}
}

func (c *Client) q(path string) string {
	u := c.base + path
	if c.project == "" || strings.Contains(path, "project=") {
		return u
	}
	scoped := strings.HasPrefix(path, "/1.0/instances") ||
		strings.HasPrefix(path, "/1.0/networks") ||
		strings.HasPrefix(path, "/1.0/images")
	if !scoped {
		return u
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return u + sep + "project=" + url.QueryEscape(c.project)
}

type opResp struct {
	ETag       string          `json:"-"`
	Type       string          `json:"type"`
	Status     string          `json:"status"`
	StatusCode int             `json:"status_code"`
	Error      string          `json:"error"`
	ErrorCode  int             `json:"error_code"`
	Operation  string          `json:"operation"`
	Metadata   json.RawMessage `json:"metadata"`
}

func (c *Client) do(method, path string, body any) (*opResp, error) {
	return c.doContext(context.Background(), method, path, body)
}

func (c *Client) doContext(ctx context.Context, method, path string, body any) (*opResp, error) {
	return c.doContextHeaders(ctx, method, path, body, nil)
}

// StatusError keeps HTTP failures distinguishable from transport/decoding errors.
// In particular, a lost connection must never be treated as a missing resource.
type StatusError struct {
	Code    int
	Message string
}

func (e *StatusError) Error() string { return e.Message }

func IsStatus(err error, code int) bool {
	var status *StatusError
	return errors.As(err, &status) && status.Code == code
}

func (c *Client) doContextHeaders(ctx context.Context, method, path string, body any, headers http.Header) (*opResp, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.q(path), rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, values := range headers {
		req.Header[key] = values
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("incus response read: %w", err)
	}
	var out opResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("incus %s %s: invalid response (HTTP %d)", method, path, res.StatusCode)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 || out.Type == "error" || out.ErrorCode >= 400 || out.StatusCode >= 400 || out.Error != "" {
		code := res.StatusCode
		if code < 400 && out.ErrorCode >= 400 {
			code = out.ErrorCode
		}
		return &out, &StatusError{Code: code, Message: fmt.Sprintf("incus %s %s failed (HTTP %d): %s", method, path, res.StatusCode, out.Error)}
	}
	if out.Type != "sync" && out.Type != "async" {
		return nil, fmt.Errorf("incus %s %s: missing response type", method, path)
	}
	if out.Type == "async" && out.Operation == "" {
		return nil, fmt.Errorf("incus %s %s: missing operation reference", method, path)
	}
	out.ETag = res.Header.Get("ETag")
	return &out, nil
}

type operationResult struct {
	Status     string          `json:"status"`
	StatusCode int             `json:"status_code"`
	Error      string          `json:"err"`
	Metadata   json.RawMessage `json:"metadata"`
}

func (c *Client) Wait(opURL string) error {
	if opURL == "" {
		return nil
	}
	_, err := c.waitOperation(opURL)
	return err
}

func (c *Client) waitOperation(opURL string) (operationResult, error) {
	return c.waitOperationContext(context.Background(), opURL)
}

func (c *Client) waitOperationContext(ctx context.Context, opURL string) (operationResult, error) {
	var result operationResult
	u, err := url.Parse(opURL)
	if err != nil || !strings.HasPrefix(u.Path, "/1.0/operations/") {
		return result, fmt.Errorf("incus: invalid operation reference")
	}
	if !strings.HasSuffix(u.Path, "/wait") {
		u.Path = strings.TrimRight(u.Path, "/") + "/wait"
	}
	query := u.Query()
	query.Set("timeout", "600")
	u.RawQuery = query.Encode()
	out, err := c.doContext(ctx, http.MethodGet, u.RequestURI(), nil)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(out.Metadata, &result); err != nil {
		return result, fmt.Errorf("incus: invalid operation result: %w", err)
	}
	if result.Error != "" || result.StatusCode != 200 {
		return result, fmt.Errorf("incus operation not successful (status %d): %s", result.StatusCode, result.Error)
	}
	return result, nil
}

func (c *Client) Ready() error {
	_, err := c.do(http.MethodGet, "/1.0", nil)
	return err
}

func (c *Client) EnsureProject() error {
	_, err := c.do(http.MethodGet, "/1.0/projects/"+c.project, nil)
	if err == nil {
		return nil
	}
	_, err = c.do(http.MethodPost, "/1.0/projects", map[string]any{
		"name": c.project,
		"config": map[string]string{
			"features.images":          "true",
			"features.profiles":        "true",
			"features.storage.volumes": "true",
		},
	})
	return err
}

type InstanceState struct {
	Status     string `json:"status"`
	StatusCode int    `json:"status_code"`
	Pid        int    `json:"pid"`
	Processes  int64  `json:"processes"`
	CPU        struct {
		Usage int64 `json:"usage"`
	} `json:"cpu"`
	Memory struct {
		Usage int64 `json:"usage"`
	} `json:"memory"`
	Network map[string]struct {
		Addresses []struct {
			Family  string `json:"family"`
			Address string `json:"address"`
		} `json:"addresses"`
		Counters struct {
			BytesReceived int64 `json:"bytes_received"`
			BytesSent     int64 `json:"bytes_sent"`
		} `json:"counters"`
	} `json:"network"`
}

func (c *Client) GetState(name string) (*InstanceState, error) {
	out, err := c.do(http.MethodGet, "/1.0/instances/"+url.PathEscape(name)+"/state", nil)
	if err != nil {
		return nil, err
	}
	var st InstanceState
	if err := json.Unmarshal(out.Metadata, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (c *Client) CreateInstance(name, image string, config map[string]string, devices map[string]map[string]string) error {
	body := map[string]any{
		"name": name,
		"type": "container",
		"source": map[string]any{
			"type":     "image",
			"mode":     "pull",
			"protocol": "simplestreams",
			"server":   "https://images.linuxcontainers.org",
			"alias":    image,
		},
		"config":   config,
		"devices":  devices,
		"profiles": []string{"default"},
	}
	out, err := c.do(http.MethodPost, "/1.0/instances", body)
	if err != nil {
		return err
	}
	return c.Wait(out.Operation)
}

func (c *Client) SetState(name, action string, force bool) error {
	out, err := c.do(http.MethodPut, "/1.0/instances/"+url.PathEscape(name)+"/state", map[string]any{
		"action":  action,
		"force":   force,
		"timeout": 30,
	})
	if err != nil {
		return &PowerError{Terminal: out != nil && out.Type == "error" && out.Operation == "", Cause: err}
	}
	if out.Type == "sync" {
		return nil
	}
	result, err := c.waitOperation(out.Operation)
	if err != nil {
		return &PowerError{Operation: out.Operation, Terminal: operationTerminal(result.StatusCode), Cause: err}
	}
	return nil
}

type PowerError struct {
	Operation string
	Terminal  bool
	Cause     error
}

func (e *PowerError) Error() string {
	return fmt.Sprintf("power operation %s (terminal=%t): %v", e.Operation, e.Terminal, e.Cause)
}
func (e *PowerError) Unwrap() error { return e.Cause }
func PowerOperationTerminal(err error) bool {
	if err == nil {
		return true
	}
	var power *PowerError
	return errors.As(err, &power) && power.Terminal
}

func (c *Client) DeleteInstance(name string) error {
	out, err := c.do(http.MethodDelete, "/1.0/instances/"+url.PathEscape(name), nil)
	if err != nil {
		return err
	}
	return c.Wait(out.Operation)
}

func (c *Client) Rebuild(name, image string) error {
	out, err := c.do(http.MethodPost, "/1.0/instances/"+url.PathEscape(name)+"/rebuild", map[string]any{
		"source": map[string]any{"type": "image", "alias": image},
	})
	if err != nil {
		return err
	}
	return c.Wait(out.Operation)
}

type ConfigOperation struct {
	ID       string `json:"id"`
	Terminal bool   `json:"terminal"`
}

// Return the operation reference before waiting so the caller can persist it.
func (c *Client) BeginConfigUpdate(name string, config map[string]string, devices map[string]map[string]string) (ConfigOperation, error) {
	body := map[string]any{}
	if config != nil {
		body["config"] = config
	}
	if devices != nil {
		body["devices"] = devices
	}
	out, err := c.do(http.MethodPatch, "/1.0/instances/"+url.PathEscape(name), body)
	if err != nil {
		// Only an explicit Incus rejection proves no operation remains active.
		return ConfigOperation{Terminal: out != nil && out.Type == "error" && out.Operation == ""}, err
	}
	return ConfigOperation{ID: out.Operation, Terminal: out.Type == "sync"}, nil
}

func operationTerminal(code int) bool {
	return code == 200 || code == 400 || code == 401
}

func (c *Client) WaitConfigOperation(id string) (bool, error) {
	result, err := c.waitOperation(id)
	return operationTerminal(result.StatusCode), err
}

func (c *Client) ConfigOperationFinished(id string) (bool, error) {
	u, err := url.Parse(id)
	if err != nil || !strings.HasPrefix(u.Path, "/1.0/operations/") {
		return false, fmt.Errorf("invalid configuration operation reference")
	}
	out, err := c.do(http.MethodGet, u.RequestURI(), nil)
	if err != nil {
		return false, err
	}
	var result operationResult
	if err := json.Unmarshal(out.Metadata, &result); err != nil {
		return false, err
	}
	return operationTerminal(result.StatusCode), nil
}

func (c *Client) GetConfig(name string) (map[string]any, error) {
	out, err := c.do(http.MethodGet, "/1.0/instances/"+url.PathEscape(name), nil)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(out.Metadata, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *Client) EnsureNetwork(name, ipv4cidr string, nat bool) error {
	_, err := c.do(http.MethodGet, "/1.0/networks/"+url.PathEscape(name), nil)
	if err == nil {
		return nil
	}
	cfg := map[string]string{
		"ipv4.address": ipv4cidr,
		"ipv4.nat":     fmt.Sprintf("%v", nat),
		"ipv6.address": "none",
	}
	_, err = c.do(http.MethodPost, "/1.0/networks", map[string]any{
		"name": name, "type": "bridge", "config": cfg,
	})
	return err
}

func (c *Client) ImageAliases() ([]string, error) {
	out, err := c.do(http.MethodGet, "/1.0/images/aliases?recursion=1", nil)
	if err != nil {
		return nil, err
	}
	var list []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out.Metadata, &list); err != nil {
		// maybe a list of strings
		var names []string
		if err2 := json.Unmarshal(out.Metadata, &names); err2 == nil {
			return names, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(list))
	for _, a := range list {
		names = append(names, a.Name)
	}
	return names, nil
}

func (c *Client) SetRootPassword(name, password string) error {
	if password == "" || strings.ContainsAny(password, "\r\n\x00") {
		return fmt.Errorf("password must be a nonempty single line")
	}
	_, err := c.Exec(name, []string{"chpasswd"}, []byte("root:"+password+"\n"))
	return err
}

func (c *Client) InstallRootKey(name, pubkey string) error {
	if strings.TrimSpace(pubkey) == "" {
		return nil
	}
	script := "mkdir -p /root/.ssh && chmod 700 /root/.ssh && touch /root/.ssh/authorized_keys && chmod 600 /root/.ssh/authorized_keys\n"
	if err := c.FilePush(name, "/tmp/.particeps-ssh.sh", []byte(script), "0700"); err != nil {
		return err
	}
	if _, err := c.Exec(name, []string{"sh", "/tmp/.particeps-ssh.sh"}, nil); err != nil {
		return err
	}
	return c.FilePush(name, "/root/.ssh/authorized_keys", []byte(strings.TrimSpace(pubkey)+"\n"), "0600")
}

func GuestIPv4(st *InstanceState) string {
	if st == nil {
		return ""
	}
	for name, n := range st.Network {
		if name == "lo" {
			continue
		}
		for _, a := range n.Addresses {
			if a.Family == "inet" && a.Address != "" {
				return a.Address
			}
		}
	}
	return ""
}

func GuestNetTotals(st *InstanceState) (rx, tx uint64) {
	if st == nil {
		return 0, 0
	}
	for name, n := range st.Network {
		if name == "lo" {
			continue
		}
		rx += uint64(n.Counters.BytesReceived)
		tx += uint64(n.Counters.BytesSent)
	}
	return rx, tx
}
