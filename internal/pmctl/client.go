package pmctl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	ConfigFile     string
	CoordinatorURL string
	TLSFiles       security.TLSFiles
	Timeout        time.Duration
}

type Client struct {
	baseURL           string
	httpClient        *http.Client
	requireSingleJSON bool
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

type RequestError struct {
	Err error
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("request coordinator: %v", e.Err)
}

func (e *RequestError) Unwrap() error {
	return e.Err
}

type DecodeError struct {
	Err error
}

func (e *DecodeError) Error() string {
	return fmt.Sprintf("decode response: %v", e.Err)
}

func (e *DecodeError) Unwrap() error {
	return e.Err
}

type Node struct {
	ID           string                       `json:"id"`
	Address      string                       `json:"address"`
	LastSeen     time.Time                    `json:"last_seen"`
	State        string                       `json:"state"`
	Capabilities []string                     `json:"capabilities"`
	Load         protocol.NodeLoad            `json:"load"`
	Certificate  security.CertificateMetadata `json:"certificate,omitempty"`
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

func newDoctorClient(cfg Config, timeout time.Duration) (*Client, error) {
	cfg.Timeout = timeout
	client, err := NewClient(cfg)
	if err != nil {
		return nil, err
	}
	client.requireSingleJSON = true
	client.httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client, nil
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
	if err != nil {
		return nil, err
	}
	if err := normalizeNodes(out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) ListJobs(ctx context.Context) ([]Job, error) {
	var out []Job
	err := c.doJSON(ctx, http.MethodGet, "/jobs", nil, &out)
	return out, err
}

func normalizeNodes(nodes []Node) error {
	for i := range nodes {
		capabilities, err := protocol.NormalizeNodeCapabilities(nodes[i].Capabilities)
		if err != nil {
			return fmt.Errorf("invalid capabilities for node %q: %w", nodes[i].ID, err)
		}
		if err := protocol.ValidateNodeLoad(nodes[i].Load); err != nil {
			return fmt.Errorf("invalid load for node %q: %w", nodes[i].ID, err)
		}
		nodes[i].Capabilities = capabilities
	}
	return nil
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
		return &RequestError{Err: err}
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
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(out); err != nil {
		return &DecodeError{Err: err}
	}
	if c.requireSingleJSON {
		var trailing any
		err := decoder.Decode(&trailing)
		if !errors.Is(err, io.EOF) {
			if err == nil {
				err = errors.New("unexpected trailing JSON value")
			}
			return &DecodeError{Err: err}
		}
	}
	return nil
}
