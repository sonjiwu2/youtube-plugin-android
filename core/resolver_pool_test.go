package core

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentCDNDNSLookupsShareBoundedTLSConnections(t *testing.T) {
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/dns-json")
		io.WriteString(w, `{"Status":0,"Answer":[{"type":1,"TTL":60,"data":"142.250.74.206"}]}`)
	}))
	origin.EnableHTTP2 = true
	origin.StartTLS()
	defer origin.Close()
	r := NewResolver(nil)
	defer r.closeIdle()
	p := dohProvider{"example.com", "8.8.8.8", "/dns-query"}
	tr := r.transport(p, true)
	roots := x509.NewCertPool()
	roots.AddCert(origin.Certificate())
	tr.TLSClientConfig = &tls.Config{RootCAs: roots, ServerName: "example.com", MinVersion: tls.VersionTLS12}
	var dials atomic.Int32
	tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		dials.Add(1)
		return (&net.Dialer{}).DialContext(ctx, "tcp", origin.Listener.Addr().String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for range 48 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ips, _, err := r.queryTypeRoute(ctx, p, "video.googlevideo.com", "A", true)
			if err != nil || len(ips) != 1 {
				t.Errorf("lookup failed: %v %v", ips, err)
			}
		}()
	}
	wg.Wait()
	if n := dials.Load(); n > 2 {
		t.Fatalf("DNS burst created %d connections; expected at most 2", n)
	}
	if r.transport(p, true) != tr {
		t.Fatal("transport was recreated per query")
	}
}
