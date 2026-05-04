package pmctl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"planetary-mesh/internal/protocol"
	"planetary-mesh/internal/security"
)

const (
	DefaultCoordinatorURL = "http://localhost:8080"
	DefaultTimeout        = 10 * time.Second
)

type Config struct {
	CoordinatorURL string
	TLSFiles       security.TLSFiles
	Timeout        time.Duration
}

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type HTTPError struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("coordinator returned %s", e.Status)
	}
	return fmt.Sprintf("coordinator returned %s: %s", e.Status, strings.TrimSpace(e.Body))
}

type Node struct {
	ID          string                       `json:"id"`
	Address     string                       `json:"address"`
	LastSeen    time.Time                    `json:"last_seen"`
	State       string                       `json:"state"`
	Certificate security.CertificateMetadata `json:"certificate,omitempty"`
}

type Job struct {
	ID              string     `json:"id"`
	Type            string     `json:"type"`
	Payload         string     `json:"payload"`
	Command         string     `json:"command,omitempty"`
	Args            []string   `json:"args,omitempty"`
	Status          string     `json:"status"`
	NodeID          string     `json:"node_id,omitempty"`
	Attempts        int        `json:"attempts"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	ExitCode        *int       `json:"exit_code,omitempty"`
	Stdout          string     `json:"stdout"`
	Stderr          string     `json:"stderr"`
	StdoutTruncated bool       `json:"stdout_truncated"`
	StderrTruncated bool       `json:"stderr_truncated"`
	LastError       string     `json:"last_error"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type createJobRequest struct {
	Type    string   `json:"type"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
}

func NewClient(cfg Config) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.CoordinatorURL), "/")
	if baseURL == "" {
		baseURL = DefaultCoordinatorURL
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	httpClient := &http.Client{Timeout: timeout}
	if cfg.TLSFiles.Configured() {
		if err := cfg.TLSFiles.ValidateComplete("PMCTL"); err != nil {
			return nil, err
		}
		tlsConfig, err := security.ClientTLSConfig(cfg.TLSFiles)
		if err != nil {
			return nil, fmt.Errorf("load TLS config: %w", err)
		}
		httpClient.Transport = &http.Transport{TLSClientConfig: tlsConfig}
	}

	return &Client{baseURL: baseURL, httpClient: httpClient}, nil
}

func NewClientWithHTTPClient(baseURL string, httpClient *http.Client) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultCoordinatorURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultTimeout}
	}
	return &Client{baseURL: baseURL, httpClient: httpClient}
}

func (c *Client) Status(ctx context.Context) (protocol.CoordinatorStatusResponse, error) {
	var out protocol.CoordinatorStatusResponse
	err := c.doJSON(ctx, http.MethodGet, "/status", nil, &out)
	return out, err
}

func (c *Client) ListNodes(ctx context.Context) ([]Node, error) {
	var out []Node
	err := c.doJSON(ctx, http.MethodGet, "/nodes", nil, &out)
	return out, err
}

func (c *Client) ListJobs(ctx context.Context) ([]Job, error) {
	var out []Job
	err := c.doJSON(ctx, http.MethodGet, "/jobs", nil, &out)
	return out, err
}

func (c *Client) GetJob(ctx context.Context, id string) (Job, error) {
	var out Job
	err := c.doJSON(ctx, http.MethodGet, "/jobs/"+id, nil, &out)
	return out, err
}

func (c *Client) CreateCommandJob(ctx context.Context, command string, args []string) (Job, error) {
	var out Job
	err := c.doJSON(ctx, http.MethodPost, "/jobs", createJobRequest{
		Type:    "command",
		Command: command,
		Args:    append([]string(nil), args...),
	}, &out)
	return out, err
}

func (c *Client) doJSON(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(in); err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = &buf
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	protocol.SetVersionHeader(req.Header)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request coordinator: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &HTTPError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       string(bodyBytes),
		}
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
