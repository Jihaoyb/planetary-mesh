package security

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/url"
	"testing"
	"time"
)

func TestTLSFilesValidateComplete(t *testing.T) {
	if err := (TLSFiles{}).ValidateComplete("AGENT"); err != nil {
		t.Fatalf("empty config should be valid: %v", err)
	}
	err := (TLSFiles{CAFile: "ca.pem"}).ValidateComplete("AGENT")
	if err == nil {
		t.Fatalf("expected partial config to fail")
	}
}

func TestNormalizeFingerprint(t *testing.T) {
	raw := "SHA256:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99"
	got, err := NormalizeFingerprint(raw)
	if err != nil {
		t.Fatalf("normalize fingerprint: %v", err)
	}
	want := "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestParseNodeAllowlists(t *testing.T) {
	identities, err := ParseIdentityAllowlist("node-1=dns:agent.local,node-1=CN:Agent One")
	if err != nil {
		t.Fatalf("parse identities: %v", err)
	}
	if len(identities["node-1"]) != 2 {
		t.Fatalf("expected repeated node identities to append, got %#v", identities)
	}

	fingerprints, err := ParseFingerprintAllowlist("node-1=aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899")
	if err != nil {
		t.Fatalf("parse fingerprints: %v", err)
	}
	if fingerprints["node-1"][0] != "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899" {
		t.Fatalf("unexpected fingerprint map: %#v", fingerprints)
	}
}

func TestCertificateMetadataAndAuthorization(t *testing.T) {
	cert := testCertificate(t)
	meta := FromCertificate(cert)
	if meta.Subject == "" || meta.SHA256Fingerprint == "" || meta.NotAfter == nil {
		t.Fatalf("expected metadata to be populated: %+v", meta)
	}
	if len(meta.DNSNames) != 1 || meta.DNSNames[0] != "agent.local" {
		t.Fatalf("unexpected DNS names: %#v", meta.DNSNames)
	}

	if !AuthorizeNode("node-1", cert, map[string][]string{"node-1": {"dns:agent.local"}}, nil) {
		t.Fatalf("expected DNS identity to authorize node")
	}
	if !AuthorizeNode("node-1", cert, nil, map[string][]string{"node-1": {Fingerprint(cert)}}) {
		t.Fatalf("expected fingerprint to authorize node")
	}
	if AuthorizeNode("node-2", cert, map[string][]string{"node-1": {"dns:agent.local"}}, nil) {
		t.Fatalf("expected wrong node id to be rejected")
	}
}

func testCertificate(t *testing.T) *x509.Certificate {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	uri, err := url.Parse("spiffe://planetary-mesh/agent-1")
	if err != nil {
		t.Fatalf("parse uri: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   "agent-1",
			Organization: []string{"Planetary Mesh Test"},
		},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		DNSNames:              []string{"agent.local"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		URIs:                  []*url.URL{uri},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	return cert
}
