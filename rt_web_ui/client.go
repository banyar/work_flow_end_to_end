package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// APIResponse is the RT External API's answer (final.html section 1, step 7).
type APIResponse struct {
	StatusCode int
	Message    string
	RunID      string
}

// Submitter sends the payload to the RT External API. An error means the API
// gave no HTTP answer at all (unreachable / timeout).
type Submitter interface {
	Submit(ctx context.Context, body []byte) (*APIResponse, error)
	URL() string
}

type apiClient struct {
	url   string
	token string
	http  *http.Client
}

func NewAPIClient(url, token string, timeout time.Duration) Submitter {
	return &apiClient{url: url, token: token, http: &http.Client{Timeout: timeout}}
}

func (c *apiClient) URL() string { return c.url }

func (c *apiClient) Submit(ctx context.Context, body []byte) (*APIResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))

	out := &APIResponse{StatusCode: resp.StatusCode}
	var parsed struct {
		Message any    `json:"message"`
		RunID   string `json:"run_id"`
	}
	if json.Unmarshal(raw, &parsed) == nil && parsed.Message != nil {
		out.Message = fmt.Sprint(parsed.Message)
		out.RunID = parsed.RunID
	} else {
		out.Message = strings.TrimSpace(string(raw))
	}
	return out, nil
}
