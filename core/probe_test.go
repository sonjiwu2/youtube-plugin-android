package core

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestProbeSelectionAndExhaustion(t *testing.T) {
	for _, success := range []bool{true, false} {
		ctx, cancel := context.WithCancel(context.Background())
		e := &Engine{ctx: ctx, opts: Options{Direct: true, AutoProbe: true}, config: Config{Strategies: []Strategy{{ID: "first"}, {ID: "second"}}}, tried: map[string]bool{"first": true}, conns: map[net.Conn]bool{}, probeWake: make(chan struct{}, 1), status: Status{State: "connecting", StrategyID: "first", Attempts: 1}}
		e.wg.Add(1)
		go e.probeLoop(func(ctx context.Context, p *childProcess) error {
			if success && e.Snapshot().StrategyID == "second" {
				return nil
			}
			return errors.New("TLS connection closed")
		})
		e.wakeProbe()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) && e.Snapshot().State != "blocked" && !e.Snapshot().Verified {
			time.Sleep(time.Millisecond)
		}
		state := e.Snapshot()
		cancel()
		e.wg.Wait()
		if state.Attempts != 2 || state.StrategyID != "second" || state.Verified != success {
			t.Fatal(state)
		}
		if !success && state.State != "blocked" {
			t.Fatal("selection did not terminate", state)
		}
	}
}

func TestProbeRejectsCaptiveAndIncompleteResponses(t *testing.T) {
	for _, tc := range []struct {
		code              int
		contentType, body string
		ok                bool
	}{
		{200, "text/html; charset=utf-8", "<!doctype html><title>YouTube</title>" + strings.Repeat(" ", 2048), true},
		{200, "text/html", "<html>login to Wi-Fi</html>" + strings.Repeat(" ", 2048), false},
		{200, "text/html", "YouTube", false},
		{200, "application/octet-stream", strings.Repeat("YouTube", 1024), false},
		{302, "text/html", strings.Repeat("YouTube", 1024), false},
		{403, "text/html", strings.Repeat("YouTube", 1024), false},
	} {
		resp := &http.Response{StatusCode: tc.code, Header: http.Header{"Content-Type": {tc.contentType}}, Body: io.NopCloser(strings.NewReader(tc.body))}
		if err := validateYouTubeResponse(resp); (err == nil) != tc.ok {
			t.Errorf("HTTP %d %q: %v", tc.code, tc.contentType, err)
		}
	}
}

func TestPreferredRoutePreservesAddresses(t *testing.T) {
	r := NewResolver(nil)
	v4, v6 := net.ParseIP("8.8.8.8"), net.ParseIP("2001:4860:4860::8888")
	until := time.Now().Add(time.Minute)
	r.cache["m.youtube.com"] = dnsEntry{[]net.IP{v4, v6}, until}
	r.prefer("m.youtube.com", v6)
	entry := r.cache["m.youtube.com"]
	if len(entry.IPs) != 2 || !entry.IPs[0].Equal(v6) || !entry.IPs[1].Equal(v4) || !entry.Until.Equal(until) {
		t.Fatal(entry)
	}
}
