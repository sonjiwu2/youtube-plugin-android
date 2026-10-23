package core

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestClientCancellationDoesNotPenalizeStrategy(t *testing.T) {
	socks, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer socks.Close()
	go func() {
		c, err := socks.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(3 * time.Second))
		b := make([]byte, 3)
		io.ReadFull(c, b)
		c.Write([]byte{5, 0})
		b = make([]byte, 10)
		io.ReadFull(c, b)
		c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 1, 187})
		io.Copy(io.Discard, c)
	}()
	e, err := Start(Options{Direct: true, DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	e.opts.Direct = false
	e.child = &childProcess{addr: socks.Addr().String()}
	e.resolver.cache["m.youtube.com"] = dnsEntry{[]net.IP{net.ParseIP("8.8.8.8")}, time.Now().Add(time.Minute)}
	defer func() { e.mu.Lock(); e.child = nil; e.mu.Unlock(); e.Stop() }()
	c, err := net.Dial("tcp", e.Address())
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(3 * time.Second))
	io.WriteString(c, "CONNECT m.youtube.com:443 HTTP/1.1\r\nHost: m.youtube.com:443\r\n\r\n")
	r := bufio.NewReader(c)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\r\n" {
			break
		}
	}
	c.Write(clientHello(t))
	c.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		n, bad := len(e.conns), e.bad
		e.mu.Unlock()
		if n == 0 {
			if bad != 0 {
				t.Fatal("browser cancellation was treated as DPI failure")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("cancelled client was not released")
}

func TestBenchmarkRejectsForeignURLs(t *testing.T) {
	e, err := Start(Options{Direct: true, DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	for _, u := range []string{"http://r1.googlevideo.com/videoplayback", "https://googlevideo.com.evil.org/", "https://user@r1.googlevideo.com/", "https://127.0.0.1/", "https://r1.googlevideo.com:8443/"} {
		if !strings.Contains(e.Measure(u), "Сначала") {
			t.Fatal(u)
		}
	}
}

func TestSOCKSUsesNumericIP(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	result := make(chan []byte, 1)
	go func() {
		c, err := listener.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(3 * time.Second))
		greeting := make([]byte, 3)
		io.ReadFull(c, greeting)
		c.Write([]byte{5, 0})
		request := make([]byte, 10)
		io.ReadFull(c, request)
		result <- request
		c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80})
	}()
	c, err := dialSOCKS(context.Background(), listener.Addr().String(), net.ParseIP("8.8.8.8"), 443)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	req := <-result
	if req[3] != 1 || string(req[4:8]) != string([]byte{8, 8, 8, 8}) {
		t.Fatalf("SOCKS did not use an IP: %v", req)
	}
}

func TestCONNECTTunnelEndToEnd(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, strings.Repeat("video", 1024)) }))
	defer origin.Close()
	socks, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer socks.Close()
	go func() {
		client, err := socks.Accept()
		if err != nil {
			return
		}
		defer client.Close()
		client.SetDeadline(time.Now().Add(5 * time.Second))
		greeting := make([]byte, 3)
		if _, err = io.ReadFull(client, greeting); err != nil {
			return
		}
		client.Write([]byte{5, 0})
		req := make([]byte, 10)
		if _, err = io.ReadFull(client, req); err != nil {
			return
		}
		remote, err := net.Dial("tcp", strings.TrimPrefix(origin.URL, "https://"))
		if err != nil {
			return
		}
		defer remote.Close()
		client.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 1, 187})
		client.SetDeadline(time.Time{})
		go func() { io.Copy(remote, client); remote.Close() }()
		io.Copy(client, remote)
	}()
	engine, err := Start(Options{Direct: true, DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	engine.opts.Direct = false
	engine.opts.AutoProbe = true
	engine.child = &childProcess{addr: socks.Addr().String()}
	engine.resolver.cache["m.youtube.com"] = dnsEntry{[]net.IP{net.ParseIP("8.8.8.8")}, time.Now().Add(time.Minute)}
	defer func() { engine.mu.Lock(); engine.child = nil; engine.mu.Unlock(); engine.Stop() }()
	proxy, _ := url.Parse("http://" + engine.Address())
	// Только локальный тестовый сервер имеет самоподписанный сертификат.
	// Внешнее SNI может отличаться от CONNECT при ECH. Туннель обязан
	// сохранить байты, а сертификат проверяет клиент (здесь локальный fixture).
	transport := &http.Transport{Proxy: http.ProxyURL(proxy), TLSClientConfig: &tls.Config{InsecureSkipVerify: true, ServerName: "www.youtube.com"}}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 5 * time.Second}
	response, err := client.Get("https://m.youtube.com/test")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || len(body) != 5120 {
		t.Fatalf("tunnel response: %d bytes, %v", len(body), err)
	}
	if engine.Snapshot().State == "ready" || engine.Snapshot().Verified {
		t.Fatal("encrypted bytes bypassed the HTTP availability check")
	}
}

func TestNoDirectFallback(t *testing.T) {
	if e, err := Start(Options{}); err == nil {
		e.Stop()
		t.Fatal("started without ciadpi")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()
	if c, err := dialSOCKS(context.Background(), addr, net.ParseIP("8.8.8.8"), 443); err == nil {
		c.Close()
		t.Fatal("closed SOCKS endpoint accepted")
	}
}
