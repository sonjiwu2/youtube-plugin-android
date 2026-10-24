package core

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestFirstFlightEscapesTCPBlackholeWithoutLosingTLSBytes(t *testing.T) {
	initial, blackhole := net.Pipe()
	defer blackhole.Close()
	hello := []byte("unchanged client hello")
	go io.Copy(io.Discard, blackhole)
	response := []byte{22, 3, 3, 0, 4, 2, 0, 0, 0, 23, 3, 3, 0, 1, 42}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := firstTLSFlight(ctx, initial, hello, func(context.Context) []net.IP {
		return []net.IP{net.ParseIP("8.8.8.8")}
	}, func(context.Context, net.IP) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			got := make([]byte, len(hello))
			if _, err := io.ReadFull(server, got); err == nil && bytes.Equal(got, hello) {
				server.Write(response)
			}
		}()
		return client, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	got, err := io.ReadAll(c)
	if err != nil || !bytes.Equal(got, response) {
		t.Fatalf("lost server bytes: %x, %v", got, err)
	}
	initial.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := initial.Read(make([]byte, 1)); err == nil {
		t.Fatal("losing route remained open")
	}
}

func TestFirstFlightCancellationClosesBlockedSocket(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := firstTLSFlight(ctx, client, []byte{1}, func(context.Context) []net.IP { return nil }, nil)
	if err == nil {
		t.Fatal("accepted a route without TLS")
	}
	server.SetReadDeadline(time.Now().Add(time.Second))
	// Cancellation callbacks run asynchronously. A pending byte may complete
	// before Close wins; wait for EOF instead of mistaking that byte for a leak.
	if _, err := io.Copy(io.Discard, server); err != nil {
		t.Fatal("cancelled socket did not close within the deadline:", err)
	}
}

type deadlineStream struct {
	net.Conn
	input     *bytes.Reader
	deadlines []time.Time
}

func (c *deadlineStream) Read(b []byte) (int, error)  { return c.input.Read(b) }
func (c *deadlineStream) Write(b []byte) (int, error) { return len(b), nil }
func (c *deadlineStream) SetReadDeadline(d time.Time) error {
	c.deadlines = append(c.deadlines, d)
	return nil
}
func (c *deadlineStream) SetWriteDeadline(time.Time) error { return nil }

func TestEstablishedStreamsSurvivePlaybackBufferAndExcludeControlTraffic(t *testing.T) {
	e, err := Start(Options{Direct: true, VPN: true})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	src := &deadlineStream{input: bytes.NewReader(make([]byte, 2048))}
	dst := &deadlineStream{}
	n, _, err := e.copyDown(dst, src, false, e.Snapshot().StrategyID, false)
	if err != io.EOF || n != 2048 {
		t.Fatalf("%d %v", n, err)
	}
	if len(src.deadlines) != 2 || time.Until(src.deadlines[1]) < 4*time.Minute {
		t.Fatal("buffered HTTP/2 connection still has a short idle timeout")
	}
	if e.Snapshot().Total != 0 {
		t.Fatal("control request polluted YouTube traffic counters")
	}
}

func TestAnalyticsCannotTriggerPlaybackStrategySwitch(t *testing.T) {
	for _, host := range []string{"s.youtube.com", "www.youtube.com", "i.ytimg.com"} {
		if playbackHost(host) {
			t.Fatal("non-playback host affects adaptation:", host)
		}
	}
	for _, host := range []string{"rr1---sn.example.googlevideo.com", "youtubei.googleapis.com"} {
		if !playbackHost(host) {
			t.Fatal("missing playback host:", host)
		}
	}
	e, err := Start(Options{Direct: true, VPN: true})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	id := e.Snapshot().StrategyID
	e.lastVideo = time.Now()
	for range 5 {
		e.failure(id, true)
	}
	if e.Snapshot().StrategyID != id {
		t.Fatal("active playback was reset")
	}
}
