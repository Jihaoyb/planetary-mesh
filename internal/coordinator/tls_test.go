package coordinator

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	agentpkg "planetary-mesh/internal/agent"
	"planetary-mesh/internal/protocol"
)

func TestSecureRegisterStoresCertificateMetadata(t *testing.T) {
	pki := newTestPKI(t)
	_, agentLeaf := pki.leaf(t, "agent-1", []string{"agent.local"}, []net.IP{net.ParseIP("127.0.0.1")})

	reg := NewNodeRegistry()
	srv := NewServerWithSecurity(
		reg,
		NewJobStore(),
		nil,
		DefaultDispatchConfig(),
		SecurityConfig{AllowedNodeIdentities: map[string][]string{"agent-1": {"dns:agent.local"}}},
		nil,
	)

	body, _ := json.Marshal(registerRequest{
		ID:           "agent-1",
		Address:      "https://agent.local:8081",
		Capabilities: []string{"role:worker"},
		Load:         protocol.NodeLoad{ActiveExecutions: 1},
	})
	req := newVersionedRequest(http.MethodPost, "/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{agentLeaf}}
	w := httptest.NewRecorder()

	srv.handleRegister(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}

	nodes, err := reg.List()
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected one node, got %d", len(nodes))
	}
	if nodes[0].Certificate.SHA256Fingerprint == "" {
		t.Fatalf("expected certificate metadata to be stored: %+v", nodes[0])
	}
	if len(nodes[0].Certificate.DNSNames) != 1 || nodes[0].Certificate.DNSNames[0] != "agent.local" {
		t.Fatalf("unexpected certificate DNS metadata: %+v", nodes[0].Certificate)
	}
	if len(nodes[0].Capabilities) != 1 || nodes[0].Capabilities[0] != "role:worker" || nodes[0].Load.ActiveExecutions != 1 {
		t.Fatalf("unexpected node metadata: %+v", nodes[0])
	}
}

func TestSecureRegisterRejectsUnauthorizedNode(t *testing.T) {
	pki := newTestPKI(t)
	_, agentLeaf := pki.leaf(t, "agent-1", []string{"agent.local"}, nil)

	reg := NewNodeRegistry()
	srv := NewServerWithSecurity(
		reg,
		NewJobStore(),
		nil,
		DefaultDispatchConfig(),
		SecurityConfig{AllowedNodeFingerprints: map[string][]string{"agent-1": {stringsOf('0', 64)}}},
		nil,
	)

	body, _ := json.Marshal(registerRequest{ID: "agent-1", Address: "https://agent.local:8081"})
	req := newVersionedRequest(http.MethodPost, "/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{agentLeaf}}
	w := httptest.NewRecorder()

	srv.handleRegister(w, req)
	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Result().StatusCode)
	}
	nodes, err := reg.List()
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("expected unauthorized node not to register, got %+v", nodes)
	}
}

func TestSecureJobResultReportRequiresAllowlistedCertificate(t *testing.T) {
	pki := newTestPKI(t)
	_, agentLeaf := pki.leaf(t, "agent-1", []string{"agent.local"}, nil)
	_, otherLeaf := pki.leaf(t, "agent-2", []string{"other.local"}, nil)

	jobs := NewJobStore()
	job, err := jobs.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if _, err := jobs.StartAttempt(job.ID, "agent-1"); err != nil {
		t.Fatalf("start attempt: %v", err)
	}
	srv := NewServerWithSecurity(
		NewNodeRegistry(),
		jobs,
		nil,
		DefaultDispatchConfig(),
		SecurityConfig{AllowedNodeIdentities: map[string][]string{"agent-1": {"dns:agent.local"}}},
		nil,
	)

	unauthorized := newResultReportRequest(t, job.ID, protocol.JobResultReportRequest{
		NodeID: "agent-1",
		Status: string(JobStatusCompleted),
		Stdout: "bad\n",
	})
	unauthorized.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{otherLeaf}}
	wUnauthorized := httptest.NewRecorder()
	srv.Mux().ServeHTTP(wUnauthorized, unauthorized)
	if wUnauthorized.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected unauthorized report 403, got %d", wUnauthorized.Result().StatusCode)
	}

	authorized := newResultReportRequest(t, job.ID, protocol.JobResultReportRequest{
		NodeID: "agent-1",
		Status: string(JobStatusCompleted),
		Stdout: "secure\n",
	})
	authorized.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{agentLeaf}}
	wAuthorized := httptest.NewRecorder()
	srv.Mux().ServeHTTP(wAuthorized, authorized)
	if wAuthorized.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected authorized report 200, got %d", wAuthorized.Result().StatusCode)
	}

	got, _, err := jobs.Get(job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.Status != JobStatusCompleted || got.Stdout != "secure\n" {
		t.Fatalf("unexpected secure reported result: %+v", got)
	}
}

func TestSecureRegisterStillChecksProtocolVersionFirst(t *testing.T) {
	srv := NewServerWithSecurity(
		NewNodeRegistry(),
		NewJobStore(),
		nil,
		DefaultDispatchConfig(),
		SecurityConfig{AllowedNodeIdentities: map[string][]string{"agent-1": {"dns:agent.local"}}},
		nil,
	)

	body, _ := json.Marshal(registerRequest{ID: "agent-1", Address: "https://agent.local:8081"})
	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleRegister(w, req)
	if w.Result().StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Result().StatusCode)
	}
}

func newResultReportRequest(t *testing.T, jobID string, payload protocol.JobResultReportRequest) *http.Request {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal result report: %v", err)
	}
	req := newVersionedRequest(http.MethodPost, "/jobs/"+jobID+"/result", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestTLSHandshakeRequiresClientCertificate(t *testing.T) {
	pki := newTestPKI(t)
	serverCert, _ := pki.leaf(t, "coordinator", []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})
	agentCert, _ := pki.leaf(t, "agent-1", nil, nil)

	serverTLS := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    pki.pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}

	noCertClient := &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    pki.pool,
		ServerName: "localhost",
	}
	if err := tlsPipeHandshake(noCertClient, serverTLS); err == nil {
		t.Fatalf("expected client without certificate to fail handshake")
	}

	withCertClient := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		RootCAs:      pki.pool,
		Certificates: []tls.Certificate{agentCert},
		ServerName:   "localhost",
	}
	if err := tlsPipeHandshake(withCertClient, serverTLS); err != nil {
		t.Fatalf("expected client certificate handshake to succeed: %v", err)
	}
}

func TestSecuredDispatchToAgent(t *testing.T) {
	pki := newTestPKI(t)
	coordinatorCert, _ := pki.leaf(t, "coordinator", nil, nil)
	agentCert, _ := pki.leaf(t, "agent-1", []string{"agent.local", "localhost"}, []net.IP{net.ParseIP("127.0.0.1")})

	agentHandler := agentpkg.MuxWithConfig(agentpkg.ExecutorConfig{
		Allowlist: map[string]string{"echo": "builtin:echo"},
		Timeout:   2 * time.Second,
	})
	agentTLSConfig := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{agentCert},
		ClientCAs:    pki.pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}
	listener := newPipeListener()
	httpServer := &http.Server{Handler: agentHandler}
	go func() {
		_ = httpServer.Serve(listener)
	}()
	defer func() {
		_ = httpServer.Close()
		_ = listener.Close()
	}()

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion:   tls.VersionTLS12,
		RootCAs:      pki.pool,
		Certificates: []tls.Certificate{coordinatorCert},
		ServerName:   "agent.local",
	}, DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		clientSide, serverSide := net.Pipe()
		serverTLS := tls.Server(serverSide, agentTLSConfig)
		select {
		case listener.conns <- serverTLS:
		case <-ctx.Done():
			_ = clientSide.Close()
			_ = serverSide.Close()
			return nil, ctx.Err()
		}

		clientTLS := tls.Client(clientSide, &tls.Config{
			MinVersion:   tls.VersionTLS12,
			RootCAs:      pki.pool,
			Certificates: []tls.Certificate{coordinatorCert},
			ServerName:   "agent.local",
		})
		if err := clientTLS.HandshakeContext(ctx); err != nil {
			_ = clientTLS.Close()
			return nil, err
		}
		return clientTLS, nil
	}}}

	reg := NewNodeRegistry()
	if _, err := reg.Register(NodeRegistration{
		ID:      "agent-1",
		Address: "https://agent.local:443",
	}); err != nil {
		t.Fatalf("register node: %v", err)
	}
	jobs := NewJobStore()
	job, err := jobs.Create(JobCreateInput{Type: "command", Command: "echo", Args: []string{"secure"}})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	srv := NewServerWithSecurity(
		reg,
		jobs,
		client,
		DispatchConfig{Timeout: time.Second, MaxAttempts: 1, BaseBackoff: time.Millisecond},
		SecurityConfig{AllowedNodeIdentities: map[string][]string{"agent-1": {"dns:agent.local"}}},
		nil,
	)
	srv.dispatchJob(job.ID)

	final, ok, err := jobs.Get(job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if !ok {
		t.Fatalf("expected job to exist")
	}
	if final.Status != JobStatusCompleted {
		t.Fatalf("expected COMPLETED, got %+v", final)
	}
	if final.Stdout == "" {
		t.Fatalf("expected stdout to be captured")
	}
}

func tlsPipeHandshake(clientConfig, serverConfig *tls.Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	clientSide, serverSide := net.Pipe()
	defer clientSide.Close()
	defer serverSide.Close()

	client := tls.Client(clientSide, clientConfig)
	server := tls.Server(serverSide, serverConfig)
	errCh := make(chan error, 2)
	go func() {
		errCh <- server.HandshakeContext(ctx)
	}()
	go func() {
		errCh <- client.HandshakeContext(ctx)
	}()

	var firstErr error
	for i := 0; i < 2; i++ {
		select {
		case err := <-errCh:
			if err != nil && firstErr == nil {
				firstErr = err
				_ = clientSide.Close()
				_ = serverSide.Close()
			}
		case <-ctx.Done():
			_ = clientSide.Close()
			_ = serverSide.Close()
			if firstErr == nil {
				firstErr = ctx.Err()
			}
		}
	}
	return firstErr
}

type pipeListener struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{
		conns:  make(chan net.Conn),
		closed: make(chan struct{}),
	}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() {
		close(l.closed)
	})
	return nil
}

func (l *pipeListener) Addr() net.Addr {
	return pipeAddr("pipe")
}

type pipeAddr string

func (a pipeAddr) Network() string { return string(a) }
func (a pipeAddr) String() string  { return string(a) }

type testPKI struct {
	caCert *x509.Certificate
	caKey  *rsa.PrivateKey
	pool   *x509.CertPool
}

func newTestPKI(t *testing.T) testPKI {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "planetary-mesh-test-ca"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA cert: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return testPKI{caCert: cert, caKey: key, pool: pool}
}

func (p testPKI) leaf(t *testing.T, commonName string, dns []string, ips []net.IP) (tls.Certificate, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName: commonName,
		},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		DNSNames:              dns,
		IPAddresses:           ips,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, p.caCert, &key.PublicKey, p.caKey)
	if err != nil {
		t.Fatalf("create leaf cert: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse leaf cert: %v", err)
	}
	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  key,
		Leaf:        leaf,
	}, leaf
}

func stringsOf(ch byte, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = ch
	}
	return string(out)
}
