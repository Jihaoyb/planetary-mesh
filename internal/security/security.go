package security

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

// TLSFiles names the certificate files needed for mTLS.
type TLSFiles struct {
	CAFile   string
	CertFile string
	KeyFile  string
}

// Configured reports whether any TLS file has been configured.
func (f TLSFiles) Configured() bool {
	return f.CAFile != "" || f.CertFile != "" || f.KeyFile != ""
}

// ValidateComplete rejects partial mTLS configuration.
func (f TLSFiles) ValidateComplete(prefix string) error {
	if !f.Configured() {
		return nil
	}
	var missing []string
	if f.CAFile == "" {
		missing = append(missing, prefix+"_TLS_CA_FILE")
	}
	if f.CertFile == "" {
		missing = append(missing, prefix+"_TLS_CERT_FILE")
	}
	if f.KeyFile == "" {
		missing = append(missing, prefix+"_TLS_KEY_FILE")
	}
	if len(missing) > 0 {
		return fmt.Errorf("partial TLS config: missing %s", strings.Join(missing, ", "))
	}
	return nil
}

// CertificateMetadata is the operator-facing identity extracted from a peer certificate.
type CertificateMetadata struct {
	Subject           string     `json:"certificate_subject,omitempty"`
	DNSNames          []string   `json:"certificate_dns_names,omitempty"`
	IPAddresses       []string   `json:"certificate_ip_addresses,omitempty"`
	URIs              []string   `json:"certificate_uris,omitempty"`
	SHA256Fingerprint string     `json:"certificate_sha256_fingerprint,omitempty"`
	NotAfter          *time.Time `json:"certificate_not_after,omitempty"`
}

// FromCertificate extracts stable metadata from an x509 leaf certificate.
func FromCertificate(cert *x509.Certificate) CertificateMetadata {
	if cert == nil {
		return CertificateMetadata{}
	}

	ips := make([]string, 0, len(cert.IPAddresses))
	for _, ip := range cert.IPAddresses {
		ips = append(ips, ip.String())
	}

	uris := make([]string, 0, len(cert.URIs))
	for _, uri := range cert.URIs {
		uris = append(uris, uri.String())
	}

	notAfter := cert.NotAfter.UTC()
	return CertificateMetadata{
		Subject:           cert.Subject.String(),
		DNSNames:          append([]string(nil), cert.DNSNames...),
		IPAddresses:       ips,
		URIs:              uris,
		SHA256Fingerprint: Fingerprint(cert),
		NotAfter:          &notAfter,
	}
}

// IdentityValues returns normalized identity strings accepted by node allowlists.
func IdentityValues(cert *x509.Certificate) []string {
	if cert == nil {
		return nil
	}

	values := []string{}
	add := func(v string) {
		v = NormalizeIdentity(v)
		if v != "" {
			values = append(values, v)
		}
	}

	if subject := cert.Subject.String(); subject != "" {
		add("subject:" + subject)
	}
	if cn := cert.Subject.CommonName; cn != "" {
		add("cn:" + cn)
		add(cn)
	}
	for _, dns := range cert.DNSNames {
		add("dns:" + dns)
		add(dns)
	}
	for _, ip := range cert.IPAddresses {
		add("ip:" + ip.String())
		add(ip.String())
	}
	for _, uri := range cert.URIs {
		add("uri:" + uri.String())
		add(uri.String())
	}
	return values
}

// Fingerprint returns the lowercase hex SHA-256 fingerprint of cert.Raw.
func Fingerprint(cert *x509.Certificate) string {
	if cert == nil {
		return ""
	}
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

// NormalizeFingerprint accepts raw hex, colon-separated hex, or sha256:hex.
func NormalizeFingerprint(raw string) (string, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	value = strings.TrimPrefix(value, "sha256:")
	value = strings.ReplaceAll(value, ":", "")
	value = strings.ReplaceAll(value, " ", "")
	if len(value) != sha256.Size*2 {
		return "", fmt.Errorf("fingerprint must be %d hex characters", sha256.Size*2)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", fmt.Errorf("invalid fingerprint: %w", err)
	}
	return value, nil
}

// NormalizeIdentity trims and lowercases an allowlist identity value.
func NormalizeIdentity(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// ParseIdentityAllowlist parses node-id=identity pairs. Repeated node ids append values.
func ParseIdentityAllowlist(raw string) (map[string][]string, error) {
	return parseNodeAllowlist(raw, func(v string) (string, error) {
		normalized := NormalizeIdentity(v)
		if normalized == "" {
			return "", fmt.Errorf("identity is empty")
		}
		return normalized, nil
	})
}

// ParseFingerprintAllowlist parses node-id=sha256 pairs. Repeated node ids append values.
func ParseFingerprintAllowlist(raw string) (map[string][]string, error) {
	return parseNodeAllowlist(raw, NormalizeFingerprint)
}

func parseNodeAllowlist(raw string, normalize func(string) (string, error)) (map[string][]string, error) {
	out := make(map[string][]string)
	if strings.TrimSpace(raw) == "" {
		return out, nil
	}

	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid allowlist entry %q", entry)
		}
		nodeID := strings.TrimSpace(parts[0])
		if nodeID == "" {
			return nil, fmt.Errorf("invalid allowlist entry %q", entry)
		}

		value, err := normalize(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid allowlist entry %q: %w", entry, err)
		}
		out[nodeID] = append(out[nodeID], value)
	}
	return out, nil
}

// AuthorizeNode checks whether a node id is allowed by either identity or fingerprint.
func AuthorizeNode(nodeID string, cert *x509.Certificate, identities, fingerprints map[string][]string) bool {
	if cert == nil {
		return false
	}

	allowedFingerprints := fingerprints[nodeID]
	if len(allowedFingerprints) > 0 {
		actual := Fingerprint(cert)
		for _, allowed := range allowedFingerprints {
			if actual == allowed {
				return true
			}
		}
	}

	allowedIdentities := identities[nodeID]
	if len(allowedIdentities) == 0 {
		return false
	}

	values := make(map[string]struct{})
	for _, value := range IdentityValues(cert) {
		values[value] = struct{}{}
	}
	for _, allowed := range allowedIdentities {
		if _, ok := values[allowed]; ok {
			return true
		}
	}
	return false
}

// LoadCAPool reads PEM CA certificates from path.
func LoadCAPool(path string) (*x509.CertPool, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("no CA certificates found in %s", path)
	}
	return pool, nil
}

// LoadKeyPair reads a certificate/key pair from disk.
func LoadKeyPair(certFile, keyFile string) (tls.Certificate, error) {
	return tls.LoadX509KeyPair(certFile, keyFile)
}

// ServerTLSConfig builds a server mTLS config.
func ServerTLSConfig(files TLSFiles, requireClientCert bool) (*tls.Config, error) {
	if err := files.ValidateComplete(""); err != nil {
		return nil, err
	}
	cert, err := LoadKeyPair(files.CertFile, files.KeyFile)
	if err != nil {
		return nil, err
	}
	pool, err := LoadCAPool(files.CAFile)
	if err != nil {
		return nil, err
	}

	cfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
	}
	if requireClientCert {
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return cfg, nil
}

// ClientTLSConfig builds a client mTLS config.
func ClientTLSConfig(files TLSFiles) (*tls.Config, error) {
	if err := files.ValidateComplete(""); err != nil {
		return nil, err
	}
	cert, err := LoadKeyPair(files.CertFile, files.KeyFile)
	if err != nil {
		return nil, err
	}
	pool, err := LoadCAPool(files.CAFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
	}, nil
}

// HostPortToURL builds a localhost URL for an address such as ":8081".
func HostPortToURL(scheme, addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	if strings.HasPrefix(addr, ":") {
		return scheme + "://localhost" + addr
	}
	host, port, err := net.SplitHostPort(addr)
	if err == nil && host == "" {
		return scheme + "://localhost:" + port
	}
	if _, err := url.ParseRequestURI(addr); err == nil && strings.Contains(addr, "://") {
		return addr
	}
	return scheme + "://" + addr
}
