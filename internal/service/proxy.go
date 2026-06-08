package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPProxy implements Service by forwarding requests to a shared daemon HTTP API.
type HTTPProxy struct {
	BaseURL     string
	WorkspaceID string
	APIToken    string
	Client      *http.Client
}

// NewHTTPProxy creates an MCP stdio proxy client for daemon mode.
func NewHTTPProxy(baseURL, workspaceID, apiToken string) *HTTPProxy {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return &HTTPProxy{
		BaseURL:     baseURL,
		WorkspaceID: workspaceID,
		APIToken:    apiToken,
		Client:      &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *HTTPProxy) SemanticSearch(req SearchRequest) (json.RawMessage, error) {
	req.WorkspaceID = p.WorkspaceID
	return p.postJSON("/v1/search", req)
}

func (p *HTTPProxy) GetIndexStatus() (json.RawMessage, error) {
	return p.getJSON("/v1/status?workspace_id=" + urlQuery(p.WorkspaceID))
}

func (p *HTTPProxy) Reindex(req ReindexRequest) (json.RawMessage, error) {
	req.WorkspaceID = p.WorkspaceID
	return p.postJSON("/v1/reindex", req)
}

func (p *HTTPProxy) CheckUpdate() (json.RawMessage, error) {
	return p.getJSON("/v1/version")
}

func (p *HTTPProxy) Ready() error {
	url := p.BaseURL + "/ready?workspace_id=" + urlQuery(p.WorkspaceID)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	p.applyHeaders(req)
	resp, err := p.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var payload struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &payload)
	if payload.Error != "" {
		return fmt.Errorf("%s", payload.Error)
	}
	return fmt.Errorf("not ready: HTTP %d", resp.StatusCode)
}

func (p *HTTPProxy) postJSON(path string, payload any) (json.RawMessage, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, p.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	p.applyHeaders(req)
	return p.do(req)
}

func (p *HTTPProxy) getJSON(path string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, p.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	p.applyHeaders(req)
	return p.do(req)
}

func (p *HTTPProxy) applyHeaders(req *http.Request) {
	if p.WorkspaceID != "" {
		req.Header.Set("X-Workspace-ID", p.WorkspaceID)
	}
	if p.APIToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIToken)
	}
}

func (p *HTTPProxy) do(req *http.Request) (json.RawMessage, error) {
	resp, err := p.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusConflict {
		return json.RawMessage(raw), ErrIndexingInProgress
	}
	if resp.StatusCode >= 400 {
		var payload struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &payload)
		if payload.Error != "" {
			return nil, fmt.Errorf("%s", payload.Error)
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return json.RawMessage(raw), nil
}

func (p *HTTPProxy) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return http.DefaultClient
}

func urlQuery(v string) string {
	return url.QueryEscape(v)
}
