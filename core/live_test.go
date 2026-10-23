package core

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"
)

// Проверяется отдельно: результат зависит от реального оператора и доступности DoH.
func TestLiveProxy(t *testing.T) {
	executable := os.Getenv("RAZGON_LIVE_CIADPI")
	if executable == "" {
		t.Skip("set RAZGON_LIVE_CIADPI for a live network test")
	}
	e, err := Start(Options{Executable: executable, FakePath: os.Getenv("RAZGON_LIVE_FAKE"), DataDir: t.TempDir(), AutoProbe: true})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	deadline := time.Now().Add(60 * time.Second)
	for !e.Snapshot().Verified && time.Now().Before(deadline) && e.Snapshot().State != "blocked" {
		time.Sleep(50 * time.Millisecond)
	}
	if !e.Snapshot().Verified {
		t.Fatal(e.Diagnostics())
	}
	p, _ := url.Parse("http://" + e.Address())
	tr := &http.Transport{Proxy: http.ProxyURL(p)}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 30 * time.Second}
	resp, err := client.Get("https://m.youtube.com")
	if err != nil {
		t.Log(e.StatusJSON())
		t.Fatal(err)
	}
	defer resp.Body.Close()
	n, err := io.Copy(io.Discard, resp.Body)
	if err != nil || resp.StatusCode != 200 || n < 1024 {
		t.Fatalf("status=%d bytes=%d err=%v", resp.StatusCode, n, err)
	}
	t.Logf("HTTPS through CONNECT → ciadpi → DoH-resolved IP: %d bytes, %s", n, e.StatusJSON())
}

func TestLiveSignedUpdate(t *testing.T) {
	executable := os.Getenv("RAZGON_LIVE_CIADPI")
	if executable == "" {
		t.Skip("set RAZGON_LIVE_CIADPI for a live update test")
	}
	pub, err := os.ReadFile("../app/src/main/assets/config-public.txt")
	if err != nil {
		t.Fatal(err)
	}
	e, err := Start(Options{Executable: executable, FakePath: os.Getenv("RAZGON_LIVE_FAKE"), DataDir: t.TempDir(), PublicKey: string(pub), RemoteURL: "https://raw.githubusercontent.com/sonjiwu2/youtube-plugin-android/main/updates/strategies.json"})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	base, _ := ParseConfig(BuiltinJSON)
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if e.Snapshot().ConfigVersion > base.Version {
			t.Log(e.StatusJSON())
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("signed remote update was not applied: " + e.StatusJSON())
}

func TestLivePresetsStart(t *testing.T) {
	executable := os.Getenv("RAZGON_LIVE_CIADPI")
	if executable == "" {
		t.Skip("set RAZGON_LIVE_CIADPI")
	}
	cfg, _ := ParseConfig(BuiltinJSON)
	for _, s := range cfg.Strategies {
		t.Run(s.ID, func(t *testing.T) {
			p, err := startChild(context.Background(), executable, os.Getenv("RAZGON_LIVE_FAKE"), s)
			if err != nil {
				t.Fatal(err)
			}
			p.stop()
		})
	}
}
