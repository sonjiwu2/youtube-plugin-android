package core

import (
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
	e, err := Start(Options{Executable: executable, FakePath: os.Getenv("RAZGON_LIVE_FAKE"), DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
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
	e, err := Start(Options{Executable: executable, FakePath: os.Getenv("RAZGON_LIVE_FAKE"), DataDir: t.TempDir(), PublicKey: string(pub), RemoteURL: "https://github.com/sonjiwu2/youtube-android-zapret/releases/download/strategies/strategies.json"})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if e.Snapshot().ConfigVersion > 1 {
			t.Log(e.StatusJSON())
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("signed remote update was not applied: " + e.StatusJSON())
}
