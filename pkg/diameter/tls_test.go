package diameter

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/diamtest"
	"github.com/fiorix/go-diameter/v4/diam/dict"
)

// newTestTLSHSS starts a TLS HSS with a freshly generated certificate;
// diamtest's built-in localhost certificate is rejected by current Go.
func newTestTLSHSS(t *testing.T, cfg *tls.Config) *diamtest.Server {
	t.Helper()
	if cfg == nil {
		cfg = &tls.Config{}
	}
	certPEM, keyPEM := generateKeyPair(t, "hss.test", x509.ExtKeyUsageServerAuth)
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Certificates = []tls.Certificate{cert}
	srv := diamtest.NewUnstartedServer(newTestHSSMux(0), dict.Default)
	srv.TLS = cfg
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func generateKeyPair(t *testing.T, cn string, usage x509.ExtKeyUsage) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

// writeClientKeyPair writes a self-signed client certificate and key as
// PEM files and returns their paths.
func writeClientKeyPair(t *testing.T) (certFile, keyFile string) {
	t.Helper()
	certPEM, keyPEM := generateKeyPair(t, "client.test", x509.ExtKeyUsageClientAuth)
	dir := t.TempDir()
	certFile = filepath.Join(dir, "client.crt")
	keyFile = filepath.Join(dir, "client.key")
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

func checkAIRSucceeds(t *testing.T, cli *Client) {
	t.Helper()
	code, err := cli.CheckSendAIR(airOpts())
	if err != nil {
		t.Fatalf("CheckSendAIR: %v", err)
	}
	if code != int64(diam.Success) {
		t.Fatalf("code = %d; want %d", code, diam.Success)
	}
}

func TestE2E_TLS_CheckSendAIR(t *testing.T) {
	hss := newTestTLSHSS(t, nil)
	opts := clientOptions(hss.Addr)
	opts.TLS = &TLSOptions{Enable: true}

	checkAIRSucceeds(t, dialClientWithOptions(t, opts))
}

func TestE2E_TLS_PresentsClientCertificate(t *testing.T) {
	var gotCN atomic.Value
	hss := newTestTLSHSS(t, &tls.Config{
		ClientAuth: tls.RequireAnyClientCert,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			cert, err := x509.ParseCertificate(rawCerts[0])
			if err != nil {
				return err
			}
			gotCN.Store(cert.Subject.CommonName)
			return nil
		},
	})
	certFile, keyFile := writeClientKeyPair(t)
	opts := clientOptions(hss.Addr)
	opts.TLS = &TLSOptions{Enable: true, CertFile: certFile, KeyFile: keyFile}

	checkAIRSucceeds(t, dialClientWithOptions(t, opts))
	if cn, _ := gotCN.Load().(string); cn != "client.test" {
		t.Fatalf("server saw client cert CN %q; want %q", cn, "client.test")
	}
}

func TestE2E_TLSDisabled_DialsPlaintext(t *testing.T) {
	hss := newTestHSS(t, 0)
	t.Cleanup(hss.Close)
	opts := clientOptions(hss.Addr)
	opts.TLS = &TLSOptions{Enable: false}

	checkAIRSucceeds(t, dialClientWithOptions(t, opts))
}
