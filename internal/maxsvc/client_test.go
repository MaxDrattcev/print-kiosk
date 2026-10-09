package maxsvc

import (
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMAXClientVerifiesCertificates(t *testing.T) {
	transport, err := maxHTTPTransport()
	if err != nil {
		t.Fatal(err)
	}
	if transport.api.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("TLS verification must remain enabled")
	}
	block, _ := pem.Decode(maxRootPEM)
	if block == nil {
		t.Fatal("missing MAX root")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !cert.IsCA || cert.Subject.CommonName != "Russian Trusted Root CA" {
		t.Fatal("unexpected MAX CA")
	}
	if err := cert.CheckSignatureFrom(cert); err != nil {
		t.Fatal(err)
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: transport.api.TLSClientConfig.RootCAs, CurrentTime: time.Now()}); err != nil {
		t.Fatal(err)
	}
	// An unrelated self-signed HTTPS service must still be rejected.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	client, err := newHTTPClient(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(server.URL)
	if err == nil {
		response.Body.Close()
		t.Fatal("untrusted server certificate was accepted")
	}
}
