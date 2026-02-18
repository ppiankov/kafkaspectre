package kafka

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTestCert(t *testing.T, dir, name string) (string, string, []byte) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "kafkaspectre-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	certPath := filepath.Join(dir, name+".crt")
	keyPath := filepath.Join(dir, name+".key")

	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	return certPath, keyPath, certPEM
}

func TestBuildSASL(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "plain",
			cfg: Config{
				AuthMechanism: "plain",
				Username:      "user",
				Password:      "pass",
			},
		},
		{
			name: "scram-sha-256",
			cfg: Config{
				AuthMechanism: "SCRAM-SHA-256",
				Username:      "user",
				Password:      "pass",
			},
		},
		{
			name: "scram-sha-512",
			cfg: Config{
				AuthMechanism: "SCRAM-SHA-512",
				Username:      "user",
				Password:      "pass",
			},
		},
		{
			name: "unsupported",
			cfg: Config{
				AuthMechanism: "GSSAPI",
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opt, err := buildSASL(tc.cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if opt == nil {
				t.Fatalf("expected non-nil option")
			}
		})
	}
}

func TestBuildSASLCaseInsensitive(t *testing.T) {
	// Verify mixed-case strings are handled by the switch
	cases := []string{"plain", "Plain", "PLAIN", "scram-sha-256", "Scram-SHA-256", "scram-sha-512", "Scram-Sha-512"}
	for _, mech := range cases {
		t.Run(mech, func(t *testing.T) {
			opt, err := buildSASL(Config{
				AuthMechanism: mech,
				Username:      "u",
				Password:      "p",
			})
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", mech, err)
			}
			if opt == nil {
				t.Fatalf("expected non-nil option for %q", mech)
			}
		})
	}
}

func TestBuildSASLUnsupportedMechanisms(t *testing.T) {
	unsupported := []string{"GSSAPI", "OAUTHBEARER", ""}
	for _, mech := range unsupported {
		if mech == "" {
			continue // empty is handled before buildSASL is called
		}
		t.Run(mech, func(t *testing.T) {
			_, err := buildSASL(Config{AuthMechanism: mech})
			if err == nil {
				t.Fatalf("expected error for unsupported mechanism %q", mech)
			}
			if !strings.Contains(err.Error(), "unsupported SASL mechanism") {
				t.Fatalf("error = %q, want 'unsupported SASL mechanism'", err.Error())
			}
		})
	}
}

func TestBuildTLSFullChain(t *testing.T) {
	// Tests a full TLS configuration with CA + client cert
	dir := t.TempDir()
	certPath, keyPath, certPEM := writeTestCert(t, dir, "full")

	caPath := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caPath, certPEM, 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}

	cfg := Config{
		TLSCertFile: certPath,
		TLSKeyFile:  keyPath,
		TLSCAFile:   caPath,
	}

	tlsCfg, err := buildTLS(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tlsCfg.Certificates) != 1 {
		t.Fatalf("expected 1 client certificate, got %d", len(tlsCfg.Certificates))
	}
	if tlsCfg.RootCAs == nil {
		t.Fatalf("expected root CAs to be set")
	}
	if tlsCfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("expected min version TLS12")
	}
}

func TestBuildTLS(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(t *testing.T, dir string) Config
		wantErr bool
		check   func(t *testing.T, cfg *tls.Config)
	}{
		{
			name: "no-files",
			setup: func(t *testing.T, dir string) Config {
				return Config{}
			},
			check: func(t *testing.T, cfg *tls.Config) {
				if cfg.MinVersion != tls.VersionTLS12 {
					t.Fatalf("expected min version TLS12, got %v", cfg.MinVersion)
				}
				if len(cfg.Certificates) != 0 {
					t.Fatalf("expected no certificates")
				}
				if cfg.RootCAs != nil {
					t.Fatalf("expected nil root CAs")
				}
			},
		},
		{
			name: "invalid-ca-path",
			setup: func(t *testing.T, dir string) Config {
				return Config{TLSCAFile: filepath.Join(dir, "missing.pem")}
			},
			wantErr: true,
		},
		{
			name: "invalid-ca-content",
			setup: func(t *testing.T, dir string) Config {
				path := filepath.Join(dir, "bad.pem")
				if err := os.WriteFile(path, []byte("not a pem"), 0o600); err != nil {
					t.Fatalf("write ca: %v", err)
				}
				return Config{TLSCAFile: path}
			},
			wantErr: true,
		},
		{
			name: "invalid-client-cert-content",
			setup: func(t *testing.T, dir string) Config {
				certPath := filepath.Join(dir, "bad-client.crt")
				keyPath := filepath.Join(dir, "bad-client.key")
				if err := os.WriteFile(certPath, []byte("not a cert"), 0o600); err != nil {
					t.Fatalf("write cert: %v", err)
				}
				if err := os.WriteFile(keyPath, []byte("not a key"), 0o600); err != nil {
					t.Fatalf("write key: %v", err)
				}
				return Config{
					TLSCertFile: certPath,
					TLSKeyFile:  keyPath,
				}
			},
			wantErr: true,
		},
		{
			name: "valid-ca",
			setup: func(t *testing.T, dir string) Config {
				_, _, certPEM := writeTestCert(t, dir, "ca")
				path := filepath.Join(dir, "ca.pem")
				if err := os.WriteFile(path, certPEM, 0o600); err != nil {
					t.Fatalf("write ca: %v", err)
				}
				return Config{TLSCAFile: path}
			},
			check: func(t *testing.T, cfg *tls.Config) {
				if cfg.RootCAs == nil {
					t.Fatalf("expected root CAs to be set")
				}
			},
		},
		{
			name: "client-cert",
			setup: func(t *testing.T, dir string) Config {
				certPath, keyPath, _ := writeTestCert(t, dir, "client")
				return Config{
					TLSCertFile: certPath,
					TLSKeyFile:  keyPath,
				}
			},
			check: func(t *testing.T, cfg *tls.Config) {
				if len(cfg.Certificates) != 1 {
					t.Fatalf("expected one certificate, got %d", len(cfg.Certificates))
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := tc.setup(t, dir)

			tlsCfg, err := buildTLS(cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tlsCfg == nil {
				t.Fatalf("expected tls config")
			}
			if tc.check != nil {
				tc.check(t, tlsCfg)
			}
		})
	}
}
