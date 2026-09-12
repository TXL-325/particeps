package incusx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

type ForwardPort struct {
	Description   string `json:"description"`
	Protocol      string `json:"protocol"`
	ListenPort    string `json:"listen_port"`
	TargetPort    string `json:"target_port"`
	TargetAddress string `json:"target_address"`
}

type Forward struct {
	ListenAddress string            `json:"listen_address"`
	Description   string            `json:"description"`
	Config        map[string]string `json:"config"`
	Ports         []ForwardPort     `json:"ports"`
}

// A transport failure or unexpected async response does not prove that the
// server has stopped writing. Keep reservations until operator reconciliation.
type ForwardUncertainError struct{ Cause error }

func (e *ForwardUncertainError) Error() string {
	return "forward write outcome is unknown; manual reconciliation required: " + e.Cause.Error()
}
func (e *ForwardUncertainError) Unwrap() error { return e.Cause }

func IsForwardUncertain(err error) bool {
	var uncertain *ForwardUncertainError
	return errors.As(err, &uncertain)
}

func forwardWriteError(err error) error {
	var status *StatusError
	if errors.As(err, &status) {
		return err
	}
	return &ForwardUncertainError{Cause: err}
}

func forwardPath(network string) string {
	return "/1.0/networks/" + url.PathEscape(network) + "/forwards"
}

func (c *Client) ListForwards(network string) ([]Forward, error) {
	out, err := c.do(http.MethodGet, forwardPath(network)+"?recursion=1", nil)
	if err != nil {
		return nil, err
	}
	var result []Forward
	if err := json.Unmarshal(out.Metadata, &result); err != nil {
		return nil, fmt.Errorf("invalid Incus forward list: %w", err)
	}
	return result, nil
}

func (c *Client) GetForward(network, listen string) (Forward, string, error) {
	out, err := c.do(http.MethodGet, forwardPath(network)+"/"+url.PathEscape(listen), nil)
	if err != nil {
		return Forward{}, "", err
	}
	var result Forward
	if err := json.Unmarshal(out.Metadata, &result); err != nil {
		return result, "", fmt.Errorf("invalid Incus forward: %w", err)
	}
	if result.ListenAddress != listen || out.ETag == "" {
		return result, "", fmt.Errorf("Incus forward identity or ETag is missing")
	}
	return result, out.ETag, nil
}

func (c *Client) CreateForward(network string, fw Forward) error {
	out, err := c.do(http.MethodPost, forwardPath(network), fw)
	if err != nil {
		return forwardWriteError(err)
	}
	if out.Type != "sync" {
		return &ForwardUncertainError{Cause: fmt.Errorf("unexpected asynchronous forward creation")}
	}
	return nil
}

func (c *Client) UpdateForward(network string, fw Forward, etag string) error {
	if etag == "" {
		return fmt.Errorf("refusing forward update without an ETag")
	}
	body := map[string]any{"description": fw.Description, "config": fw.Config, "ports": fw.Ports}
	out, err := c.doContextHeaders(context.Background(), http.MethodPut,
		forwardPath(network)+"/"+url.PathEscape(fw.ListenAddress), body, http.Header{"If-Match": []string{etag}})
	if err != nil {
		return forwardWriteError(err)
	}
	if out.Type != "sync" {
		return &ForwardUncertainError{Cause: fmt.Errorf("unexpected asynchronous forward update")}
	}
	return nil
}
