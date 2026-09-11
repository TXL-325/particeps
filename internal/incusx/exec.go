package incusx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func (c *Client) Exec(name string, command []string, stdin []byte) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	stream := len(stdin) > 0
	body := map[string]any{
		"command":            command,
		"wait-for-websocket": stream,
		"interactive":        false,
		"record-output":      !stream,
	}
	out, err := c.doContext(ctx, http.MethodPost, "/1.0/instances/"+url.PathEscape(name)+"/exec", body)
	if err != nil {
		return "", err
	}
	if !stream {
		result, err := c.waitOperationContext(ctx, out.Operation)
		if err != nil {
			return "", err
		}
		return "", commandResult(result)
	}
	var operation struct {
		Metadata struct {
			FDs map[string]string `json:"fds"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(out.Metadata, &operation); err != nil {
		return "", fmt.Errorf("exec: invalid websocket metadata")
	}
	connections := make(map[string]*websocket.Conn)
	defer func() {
		for _, conn := range connections {
			_ = conn.Close()
		}
	}()
	for _, fd := range []string{"0", "1", "2"} {
		secret := operation.Metadata.FDs[fd]
		if secret == "" {
			return "", fmt.Errorf("exec: missing websocket descriptor %s", fd)
		}
		conn, err := c.execSocket(ctx, out.Operation, secret)
		if err != nil {
			return "", fmt.Errorf("exec: websocket connection failed: %w", err)
		}
		deadline, _ := ctx.Deadline()
		_ = conn.SetReadDeadline(deadline)
		_ = conn.SetWriteDeadline(deadline)
		conn.SetReadLimit(4 << 20)
		connections[fd] = conn
	}
	type streamResult struct {
		data string
		err  error
	}
	outputs := make(map[string]chan streamResult)
	for _, fd := range []string{"1", "2"} {
		ch := make(chan streamResult, 1)
		outputs[fd] = ch
		go func(conn *websocket.Conn) {
			data, err := readExecStream(conn)
			ch <- streamResult{data, err}
		}(connections[fd])
	}
	if err := connections["0"].WriteMessage(websocket.BinaryMessage, stdin); err != nil {
		return "", err
	}
	// Incus terminates a stream with an empty text frame, not raw TCP bytes.
	if err := connections["0"].WriteMessage(websocket.TextMessage, nil); err != nil {
		return "", err
	}
	result, err := c.waitOperationContext(ctx, out.Operation)
	if err != nil {
		return "", err
	}
	var stdout string
	for _, fd := range []string{"1", "2"} {
		select {
		case output := <-outputs[fd]:
			if output.err != nil {
				return "", output.err
			}
			if fd == "1" {
				stdout = output.data
			}
		case <-ctx.Done():
			return "", fmt.Errorf("exec output: %w", ctx.Err())
		}
	}
	return stdout, commandResult(result)
}

func commandResult(result operationResult) error {
	var metadata struct {
		ExitCode *int `json:"return"`
	}
	if err := json.Unmarshal(result.Metadata, &metadata); err != nil || metadata.ExitCode == nil {
		return fmt.Errorf("exec: missing command exit status")
	}
	if *metadata.ExitCode != 0 {
		return fmt.Errorf("exec: command exited with status %d", *metadata.ExitCode)
	}
	return nil
}

func (c *Client) execSocket(ctx context.Context, operation, secret string) (*websocket.Conn, error) {
	op, err := url.Parse(operation)
	if err != nil || !strings.HasPrefix(op.Path, "/1.0/operations/") {
		return nil, fmt.Errorf("invalid exec operation")
	}
	u, err := url.Parse(c.base)
	if err != nil {
		return nil, err
	}
	u.Path = strings.TrimRight(op.Path, "/") + "/websocket"
	query := op.Query()
	query.Set("secret", secret)
	u.RawQuery = query.Encode()
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	if transport, ok := c.http.Transport.(*http.Transport); ok {
		dialer.NetDialContext = transport.DialContext
		dialer.TLSClientConfig = transport.TLSClientConfig
	}
	conn, response, err := dialer.DialContext(ctx, u.String(), nil)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	return conn, err
}

func readExecStream(conn *websocket.Conn) (string, error) {
	var output strings.Builder
	for {
		kind, data, err := conn.ReadMessage()
		if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) || err == io.EOF {
			return output.String(), nil
		}
		if err != nil {
			return "", fmt.Errorf("exec stream: %w", err)
		}
		if kind == websocket.TextMessage {
			return output.String(), nil
		}
		if output.Len()+len(data) > 4<<20 {
			return "", fmt.Errorf("exec output limit exceeded")
		}
		output.Write(data)
	}
}

func (c *Client) FilePush(name, path string, content []byte, mode string) error {
	req, err := http.NewRequest(http.MethodPost, c.q("/1.0/instances/"+url.PathEscape(name)+"/files?path="+url.QueryEscape(path)), strings.NewReader(string(content)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if mode != "" {
		req.Header.Set("X-Incus-mode", mode)
		req.Header.Set("X-LXD-mode", mode)
	}
	req.Header.Set("X-Incus-uid", "0")
	req.Header.Set("X-Incus-gid", "0")
	req.Header.Set("X-Incus-type", "file")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("file push: %s %s", res.Status, b)
	}
	return nil
}
