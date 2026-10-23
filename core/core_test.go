package core

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPublishedConfigSignature(t *testing.T) {
	data, err := os.ReadFile("../updates/strategies.json")
	if err != nil {
		t.Fatal(err)
	}
	sig, err := os.ReadFile("../updates/strategies.json.sig")
	if err != nil {
		t.Fatal(err)
	}
	pub, err := os.ReadFile("../app/src/main/assets/config-public.txt")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	expiry, err := time.Parse(time.RFC3339, cfg.Expires)
	if err != nil {
		t.Fatal(err)
	}
	base, _ := ParseConfig(BuiltinJSON)
	if _, err = VerifyConfig(data, sig, string(pub), base.Version, expiry.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func TestHostlist(t *testing.T) {
	for _, tc := range []struct {
		host string
		want bool
	}{{"r1.googlevideo.com", true}, {"GOOGLEVIDEO.COM.", true}, {"evilgooglevideo.com", false}, {"youtube.com.evil.org", false}, {"youtube.com", true}, {"localhost", false}} {
		if got := MatchHost(tc.host, DefaultHosts); got != tc.want {
			t.Errorf("%s: %v", tc.host, got)
		}
	}
	if MatchHost("sub.example.com", []string{"^example.com"}) {
		t.Fatal("exact rule matched subdomain")
	}
}
func TestPublicIP(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "::1", "10.0.0.1", "100.64.0.1", "169.254.169.254", "192.168.1.1", "::ffff:127.0.0.1", "2001:db8::1"} {
		if publicIP(net.ParseIP(s)) {
			t.Fatal(s)
		}
	}
	if !publicIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("public IP rejected")
	}
}

func clientHello(t *testing.T) []byte {
	t.Helper()
	a, b := net.Pipe()
	defer b.Close()
	go func() {
		defer a.Close()
		_ = tls.Client(a, &tls.Config{ServerName: "m.youtube.com", MinVersion: tls.VersionTLS12}).Handshake()
	}()
	h := make([]byte, 65536)
	b.SetReadDeadline(time.Now().Add(time.Second))
	n, err := b.Read(h)
	if err != nil {
		t.Fatal(err)
	}
	return h[:n]
}
func TestClientHello(t *testing.T) {
	wire := clientHello(t)
	_, host, err := ReadClientHello(bytes.NewReader(wire))
	if err != nil || host != "m.youtube.com" {
		t.Fatalf("%q %v", host, err)
	}
	for n := 0; n < len(wire); n++ {
		if _, _, err := ReadClientHello(bytes.NewReader(wire[:n])); err == nil {
			t.Fatalf("accepted truncated record %d", n)
		}
	}
}
func TestFragmentedHello(t *testing.T) {
	wire := clientHello(t)
	body := wire[5:]
	var fragment []byte
	for _, part := range [][]byte{body[:17], body[17:]} {
		fragment = append(fragment, 22, 3, 1, byte(len(part)>>8), byte(len(part)))
		fragment = append(fragment, part...)
	}
	_, host, err := ReadClientHello(bytes.NewReader(fragment))
	if err != nil || host != "m.youtube.com" {
		t.Fatal(host, err)
	}
}
func FuzzSNI(f *testing.F) {
	f.Add([]byte{1, 0, 0, 0})
	f.Fuzz(func(t *testing.T, b []byte) { ParseSNI(b) })
}
func TestSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	c, _ := ParseConfig(BuiltinJSON)
	c.Expires = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	data, _ := json.Marshal(c)
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data)))
	key := base64.StdEncoding.EncodeToString(pub)
	if _, err := VerifyConfig(data, sig, key, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyConfig(append(data, ' '), sig, key, 1, time.Now()); err == nil {
		t.Fatal("accepted tampering")
	}
	if _, err := VerifyConfig(data, sig, key, c.Version+1, time.Now()); err == nil {
		t.Fatal("accepted rollback")
	}
	if _, err := VerifyConfig(data, sig, key, 1, time.Now().Add(2*time.Hour)); err == nil {
		t.Fatal("accepted expired config")
	}
}
func TestConfigRejectsUnsafeArgs(t *testing.T) {
	for _, args := range [][]string{{"-i", "0.0.0.0"}, {"-D", "1"}, {"-l", "/etc/passwd"}, {"-t", "255"}, {"-P", "/tmp/file"}, {"-s"}} {
		if validateArgs(args) == nil {
			t.Fatal(args)
		}
	}
	if _, err := ParseConfig(BuiltinJSON); err != nil {
		t.Fatal(err)
	}
}
func TestDoHFallback(t *testing.T) {
	r := NewResolver(map[string][]string{"m.youtube.com": {"142.250.185.206"}})
	r.providers = nil
	ips, err := r.Resolve(context.Background(), "m.youtube.com")
	if err != nil || len(ips) != 1 {
		t.Fatal(ips, err)
	}
	if _, err = r.Resolve(context.Background(), "r1.googlevideo.com"); err == nil {
		t.Fatal("invented CDN IP")
	}
	if _, err = r.Resolve(context.Background(), "127.0.0.1"); err == nil {
		t.Fatal("private IP allowed")
	}
}
func TestProxyRejectAndLifecycle(t *testing.T) {
	e, err := Start(Options{Direct: true, DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("tcp", e.Address())
	if err != nil {
		t.Fatal(err)
	}
	c.Write([]byte("CONNECT 127.0.0.1:443 HTTP/1.1\r\nHost: 127.0.0.1:443\r\n\r\n"))
	buf := make([]byte, 100)
	n, _ := c.Read(buf)
	c.Close()
	if !strings.Contains(string(buf[:n]), "403") {
		t.Fatal(string(buf[:n]))
	}
	e.Stop()
	if _, err = net.DialTimeout("tcp", e.Address(), 100*time.Millisecond); err == nil {
		t.Fatal("listener still open")
	}
}
func TestStrategySelection(t *testing.T) {
	e, err := Start(Options{Direct: true, DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	first := e.Snapshot().StrategyID
	e.failure(first, true)
	if e.Snapshot().StrategyID != first {
		t.Fatal("switched after one failure")
	}
	e.failure(first, true)
	if e.Snapshot().StrategyID != first {
		t.Fatal("switched before three failures")
	}
	e.failure(first, true)
	if e.Snapshot().StrategyID == first {
		t.Fatal("did not switch")
	}
	e.failure(first, true)
	if e.bad != 0 {
		t.Fatal("stale connection affected new strategy")
	}
	for i := 0; i <= len(e.config.Strategies); i++ {
		e.Next()
	}
	if e.Snapshot().State != "blocked" {
		t.Fatal("unbounded selection")
	}
}
