package pmctl

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"planetary-mesh/internal/coordinator"
	"planetary-mesh/internal/protocol"
	"planetary-mesh/internal/security"
)

func TestSmokeFlowAgainstCoordinatorHandler(t *testing.T) {
	agentClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/execute" {
			return textResponse(http.StatusNotFound, "not found"), nil
		}
		if r.Header.Get(protocol.HeaderName) != protocol.Version {
			return textResponse(http.StatusConflict, "protocol version mismatch"), nil
		}
		return jsonResponse(http.StatusOK, protocol.ExecuteResponse{Status: "ok", Stdout: "hello smoke\n"}), nil
	})}

	registry := coordinator.NewNodeRegistry()
	jobs := coordinator.NewJobStore()
	srv := coordinator.NewServerWithRuntime(
		registry,
		jobs,
		agentClient,
		coordinator.DispatchConfig{Timeout: time.Second, MaxAttempts: 1, BaseBackoff: time.Millisecond},
		coordinator.SecurityConfig{},
		coordinator.RuntimeConfig{StorageBackend: "in_memory"},
		nil,
	)
	mux := srv.Mux()
	registerSmokeNode(t, mux)

	client := NewClientWithHTTPClient("http://coordinator.test", &http.Client{Transport: handlerTransport{handler: mux}})
	ctx := context.Background()

	status, err := client.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Status != "ok" || status.StorageBackend != "in_memory" {
		t.Fatalf("unexpected status: %+v", status)
	}

	nodes, err := client.ListNodes(ctx)
	if err != nil {
		t.Fatalf("nodes list: %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != "agent-smoke" {
		t.Fatalf("unexpected nodes: %+v", nodes)
	}

	created, err := client.CreateCommandJob(ctx, "echo", []string{"hello smoke"})
	if err != nil {
		t.Fatalf("submit command: %v", err)
	}
	completed := waitForSmokeJob(t, client, created.ID)
	if completed.Status != string(coordinator.JobStatusCompleted) || completed.Stdout != "hello smoke\n" {
		t.Fatalf("unexpected completed job: %+v", completed)
	}

	listed, err := client.ListJobs(ctx)
	if err != nil {
		t.Fatalf("jobs list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("unexpected jobs list: %+v", listed)
	}

	var out bytes.Buffer
	if err := runCommandWithClient(ctx, clientAdapter{client}, []string{"jobs", "inspect", created.ID}, &out, false); err != nil {
		t.Fatalf("jobs inspect command: %v", err)
	}
	if !strings.Contains(out.String(), "hello smoke") {
		t.Fatalf("expected job output in inspect result, got:\n%s", out.String())
	}
}

func TestDoctorUsesOnlyVersionedReadOnlyCoordinatorEndpoints(t *testing.T) {
	registry := coordinator.NewNodeRegistry()
	if _, err := registry.Register(coordinator.NodeRegistration{
		ID:      "doctor-agent",
		Address: "http://private-doctor-agent.local:8081",
	}); err != nil {
		t.Fatalf("register node: %v", err)
	}
	jobs := coordinator.NewJobStore()
	srv := coordinator.NewServerWithRuntime(
		registry,
		jobs,
		nil,
		coordinator.DefaultDispatchConfig(),
		coordinator.SecurityConfig{},
		coordinator.RuntimeConfig{StorageBackend: "in_memory"},
		nil,
	)

	var methods []string
	var paths []string
	recordingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		paths = append(paths, r.URL.Path)
		if r.Header.Get(protocol.HeaderName) != protocol.Version {
			t.Errorf("request %s %s missing protocol version", r.Method, r.URL.Path)
		}
		srv.Mux().ServeHTTP(w, r)
	})
	client := NewClientWithHTTPClient("http://coordinator.test", &http.Client{
		Transport: handlerTransport{handler: recordingHandler},
	})
	client.requireSingleJSON = true

	report := newDoctorReport(doctorOptions{Timeout: DefaultTimeout})
	report.addCheck(validConfigurationCheck())
	result := runDoctorChecks(context.Background(), client, validatedDoctorConfig{}, &report)
	if result.interrupted {
		t.Fatal("doctor unexpectedly interrupted")
	}
	report.aggregate()

	if report.OverallStatus != doctorStatusPass {
		t.Fatalf("expected PASS, got %+v", report)
	}
	if got, want := strings.Join(methods, ","), "GET,GET"; got != want {
		t.Fatalf("unexpected methods: got %q want %q", got, want)
	}
	if got, want := strings.Join(paths, ","), "/status,/nodes"; got != want {
		t.Fatalf("unexpected paths: got %q want %q", got, want)
	}
	listed, err := jobs.List()
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("doctor created jobs: %+v", listed)
	}
}

func TestNewClientLoadsSecureConfig(t *testing.T) {
	files := writeTestTLSFiles(t)
	client, err := NewClient(Config{
		CoordinatorURL: "https://coordinator.test",
		TLSFiles:       files,
	})
	if err != nil {
		t.Fatalf("new secure client: %v", err)
	}
	if client.httpClient.Transport == nil {
		t.Fatalf("expected secure client transport")
	}
}

func registerSmokeNode(t *testing.T, handler http.Handler) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"id": "agent-smoke", "address": "agent.local:8081"})
	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	protocol.SetVersionHeader(req.Header)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("register smoke node: %d", w.Result().StatusCode)
	}
}

func waitForSmokeJob(t *testing.T, client *Client, id string) Job {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		job, err := client.GetJob(context.Background(), id)
		if err == nil && job.Status == string(coordinator.JobStatusCompleted) {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, err := client.GetJob(context.Background(), id)
	if err != nil {
		t.Fatalf("get final smoke job: %v", err)
	}
	t.Fatalf("timed out waiting for job completion: %+v", job)
	return Job{}
}

type handlerTransport struct {
	handler http.Handler
}

func (t handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.RequestURI = ""
	w := httptest.NewRecorder()
	t.handler.ServeHTTP(w, clone)
	return w.Result(), nil
}

func writeTestTLSFiles(t *testing.T) security.TLSFiles {
	t.Helper()

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "pmctl-test-ca"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create CA cert: %v", err)
	}

	clientKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate client key: %v", err)
	}
	clientTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "pmctl-operator"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	clientDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, caTemplate, &clientKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create client cert: %v", err)
	}

	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	certFile := filepath.Join(dir, "client.pem")
	keyFile := filepath.Join(dir, "client-key.pem")
	writePEM(t, caFile, "CERTIFICATE", caDER)
	writePEM(t, certFile, "CERTIFICATE", clientDER)
	writePEM(t, keyFile, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(clientKey))
	return security.TLSFiles{CAFile: caFile, CertFile: certFile, KeyFile: keyFile}
}

func writePEM(t *testing.T, path, typ string, der []byte) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer file.Close()
	if err := pem.Encode(file, &pem.Block{Type: typ, Bytes: der}); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
