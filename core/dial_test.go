package core

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestDialCandidatesSkipsBlackhole(t *testing.T) {
	cancelled := make(chan struct{})
	a, b := net.Pipe()
	defer b.Close()
	start := time.Now()
	c, err := dialCandidates(context.Background(), []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("1.1.1.1")}, func(ctx context.Context, ip net.IP) (net.Conn, error) {
		if ip.String() == "8.8.8.8" {
			<-ctx.Done()
			close(cancelled)
			return nil, ctx.Err()
		}
		return a, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if time.Since(start) > time.Second {
		t.Fatal("working address waited for blackhole")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("losing dial was not cancelled")
	}
}

func TestDialCandidatesKeepsBothFamilies(t *testing.T) {
	var ips []net.IP
	for range 10 {
		ips = append(ips, net.ParseIP("8.8.8.8"))
	}
	ips = append(ips, net.ParseIP("2001:4860:4860::8888"))
	a, b := net.Pipe()
	defer b.Close()
	c, err := dialCandidates(context.Background(), ips, func(ctx context.Context, ip net.IP) (net.Conn, error) {
		if ip.To4() == nil {
			return a, nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}
