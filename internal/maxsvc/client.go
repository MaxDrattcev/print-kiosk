package maxsvc

import (
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"fmt"
	"net/http"
	"sync"
	"time"

	maxbot "github.com/max-messenger/max-bot-api-client-go/v2"
)

// Official Gosuslugi certificate, downloaded over verified HTTPS from
// https://gu-st.ru/content/lending/russian_trusted_root_ca_pem.crt
// SHA-256: D26D2D0231B7C39F92CC738512BA54103519E4405D68B5BD703E9788CA8ECF31
//
//go:embed certs/russian_trusted_root_ca.pem
var maxRootPEM []byte

// Additional trust applies only to the MAX API, including after redirects.
// Downloads on other domains continue to use normal system trust.
type maxTransport struct {
	system *http.Transport
	api    *http.Transport
}

func (t *maxTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == "https" && req.URL.Hostname() == "platform-api2.max.ru" {
		return t.api.RoundTrip(req)
	}
	return t.system.RoundTrip(req)
}

var maxHTTPTransport = sync.OnceValues(func() (*maxTransport, error) {
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("системные сертификаты MAX: %w", err)
	}
	if !roots.AppendCertsFromPEM(maxRootPEM) {
		return nil, fmt.Errorf("не удалось загрузить корневой сертификат MAX")
	}
	system := http.DefaultTransport.(*http.Transport).Clone()
	api := system.Clone()
	api.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	return &maxTransport{system: system, api: api}, nil
})

func newHTTPClient(timeout time.Duration) (*http.Client, error) {
	transport, err := maxHTTPTransport()
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: transport, Timeout: timeout}, nil
}
func NewAPI(token string) (*maxbot.Api, error) {
	client, err := newHTTPClient(30 * time.Second)
	if err != nil {
		return nil, err
	}
	return maxbot.NewApi(token, maxbot.WithHTTPClient(client))
}
