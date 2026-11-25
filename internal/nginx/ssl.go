package nginx

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/http/webroot"
	"github.com/go-acme/lego/v4/registration"

	"door/internal/sys"
)

type SSLCert struct {
	ID        int64     `json:"id"`
	Domain    string    `json:"domain"`
	Provider  string    `json:"provider"`
	Email     string    `json:"email"`
	CertPath  string    `json:"cert_path"`
	KeyPath   string    `json:"key_path"`
	ExpiresAt time.Time `json:"expires_at"`
	AutoRenew bool      `json:"auto_renew"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type IssueSSLInput struct {
	Domain   string `json:"domain"`
	Provider string `json:"provider"` // selfsigned | manual | letsencrypt
	Email    string `json:"email"`
	CertPEM  string `json:"cert_pem"`
	KeyPEM   string `json:"key_pem"`
}

func (s *Service) ListCerts(ctx context.Context) ([]SSLCert, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, domain, provider, email, cert_path, key_path, expires_at, auto_renew, status, created_at
FROM ssl_certs ORDER BY id DESC
`)
	if err != nil {
		return nil, fmt.Errorf("query ssl certs: %w", err)
	}
	defer rows.Close()

	out := []SSLCert{}
	for rows.Next() {
		var c SSLCert
		var ar int
		if err := rows.Scan(&c.ID, &c.Domain, &c.Provider, &c.Email, &c.CertPath, &c.KeyPath, &c.ExpiresAt, &ar, &c.Status, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan ssl cert: %w", err)
		}
		c.AutoRenew = ar == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) GetCert(ctx context.Context, id int64) (SSLCert, error) {
	var c SSLCert
	var ar int
	err := s.db.QueryRowContext(ctx, `
SELECT id, domain, provider, email, cert_path, key_path, expires_at, auto_renew, status, created_at
FROM ssl_certs WHERE id = ?
`, id).Scan(&c.ID, &c.Domain, &c.Provider, &c.Email, &c.CertPath, &c.KeyPath, &c.ExpiresAt, &ar, &c.Status, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SSLCert{}, ErrCertNotFound
	}
	if err != nil {
		return SSLCert{}, err
	}
	c.AutoRenew = ar == 1
	return c, nil
}

func (s *Service) IssueSSL(ctx context.Context, in IssueSSLInput) (SSLCert, error) {
	in.Domain = strings.ToLower(strings.TrimSpace(in.Domain))
	in.Provider = strings.ToLower(strings.TrimSpace(in.Provider))
	in.Email = strings.TrimSpace(in.Email)

	if err := ValidateDomain(in.Domain); err != nil {
		return SSLCert{}, err
	}

	var (
		certPEM   []byte
		keyPEM    []byte
		expiresAt time.Time
		autoRenew bool
		err       error
	)

	switch in.Provider {
	case "selfsigned":
		certPEM, keyPEM, expiresAt, err = generateSelfSignedCert(in.Domain)
	case "manual":
		certPEM = []byte(strings.TrimSpace(in.CertPEM) + "\n")
		keyPEM = []byte(strings.TrimSpace(in.KeyPEM) + "\n")
		expiresAt, err = validateKeyPair(certPEM, keyPEM)
	case "letsencrypt":
		if in.Email == "" || !strings.Contains(in.Email, "@") {
			return SSLCert{}, fmt.Errorf("%w: valid email is required for letsencrypt", ErrInvalidNginx)
		}
		certPEM, keyPEM, expiresAt, err = s.obtainLetsEncryptCert(in.Domain, in.Email)
		autoRenew = true
	default:
		return SSLCert{}, fmt.Errorf("%w: unsupported ssl provider %q", ErrInvalidNginx, in.Provider)
	}
	if err != nil {
		return SSLCert{}, err
	}

	safeDomain := strings.ReplaceAll(in.Domain, "*", "_wildcard_")
	relCert := filepath.Join("ssl", safeDomain, "fullchain.pem")
	relKey := filepath.Join("ssl", safeDomain, "privkey.pem")
	if err := sys.WriteFileRoot(s.paths.NginxDir, relCert, certPEM, 0o644); err != nil {
		return SSLCert{}, err
	}
	if err := sys.WriteFileRoot(s.paths.NginxDir, relKey, keyPEM, 0o600); err != nil {
		return SSLCert{}, err
	}

	certPath, err := sys.SafeJoin(s.paths.NginxDir, relCert)
	if err != nil {
		return SSLCert{}, err
	}
	keyPath, err := sys.SafeJoin(s.paths.NginxDir, relKey)
	if err != nil {
		return SSLCert{}, err
	}

	arInt := 0
	if autoRenew {
		arInt = 1
	}

	res, err := s.db.ExecContext(ctx, `
INSERT INTO ssl_certs (domain, provider, email, cert_path, key_path, expires_at, auto_renew, status)
VALUES (?, ?, ?, ?, ?, ?, ?, 'active')
`, in.Domain, in.Provider, in.Email, certPath, keyPath, expiresAt, arInt)
	if err != nil {
		return SSLCert{}, fmt.Errorf("save ssl cert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return SSLCert{}, err
	}
	return s.GetCert(ctx, id)
}

func (s *Service) DeleteCert(ctx context.Context, id int64) error {
	c, err := s.GetCert(ctx, id)
	if err != nil {
		return err
	}
	_ = os.Remove(c.CertPath)
	_ = os.Remove(c.KeyPath)
	_, err = s.db.ExecContext(ctx, `DELETE FROM ssl_certs WHERE id = ?`, id)
	return err
}

func generateSelfSignedCert(domain string) ([]byte, []byte, time.Time, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("generate ecdsa key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(365 * 24 * time.Hour)
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   domain,
			Organization: []string{"Door Panel Self-Signed"},
		},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              expiresAt,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{domain},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("create certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	return certPEM, keyPEM, expiresAt, nil
}

func validateKeyPair(certPEM, keyPEM []byte) (time.Time, error) {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: invalid certificate/key pair: %v", ErrInvalidNginx, err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: parse certificate: %v", ErrInvalidNginx, err)
	}
	return leaf.NotAfter, nil
}

type acmeUser struct {
	Email        string
	Registration *registration.Resource
	key          crypto.PrivateKey
}

func (u *acmeUser) GetEmail() string                        { return u.Email }
func (u *acmeUser) GetRegistration() *registration.Resource { return u.Registration }
func (u *acmeUser) GetPrivateKey() crypto.PrivateKey        { return u.key }

func (s *Service) obtainLetsEncryptCert(domain, email string) ([]byte, []byte, time.Time, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	user := &acmeUser{Email: email, key: priv}
	cfg := lego.NewConfig(user)
	cfg.Certificate.KeyType = certcrypto.RSA2048

	client, err := lego.NewClient(cfg)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("create acme client: %w", err)
	}

	if err := os.MkdirAll(s.acmeWebroot(), 0o750); err != nil {
		return nil, nil, time.Time{}, err
	}
	provider, err := webroot.NewHTTPProvider(s.acmeWebroot())
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("create http01 webroot provider: %w", err)
	}
	if err := client.Challenge.SetHTTP01Provider(provider, http01.SetDelay(time.Second)); err != nil {
		return nil, nil, time.Time{}, err
	}

	reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("acme register: %w", err)
	}
	user.Registration = reg

	res, err := client.Certificate.Obtain(certificate.ObtainRequest{
		Domains: []string{domain},
		Bundle:  true,
	})
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("acme obtain certificate: %w", err)
	}
	expiresAt, err := validateKeyPair(res.Certificate, res.PrivateKey)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	return res.Certificate, res.PrivateKey, expiresAt, nil
}
