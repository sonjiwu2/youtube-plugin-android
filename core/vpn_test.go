package core

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestVPNDNS(t *testing.T) {
	name, _ := dnsmessage.NewName("youtube.com.")
	for _, test := range []struct {
		kind dnsmessage.Type
		fail bool
		want int
		code dnsmessage.RCode
	}{
		{dnsmessage.TypeA, false, 1, dnsmessage.RCodeSuccess},
		{dnsmessage.TypeAAAA, false, 1, dnsmessage.RCodeSuccess},
		{dnsmessage.Type(65), false, 0, dnsmessage.RCodeSuccess},
		{dnsmessage.TypeA, true, 0, dnsmessage.RCodeServerFailure},
	} {
		query := dnsmessage.Message{Header: dnsmessage.Header{ID: 271, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: name, Type: test.kind, Class: dnsmessage.ClassINET}}}
		wire, _ := query.Pack()
		got := answerDNS(context.Background(), wire, func(_ context.Context, host string) ([]net.IP, error) {
			if host != "youtube.com" {
				t.Fatal(host)
			}
			if test.fail {
				return nil, errors.New("offline")
			}
			return []net.IP{net.ParseIP("142.250.74.206"), net.ParseIP("2a00:1450:4001:80f::200e"), net.ParseIP("127.0.0.1")}, nil
		})
		var reply dnsmessage.Message
		if err := reply.Unpack(got); err != nil {
			t.Fatal(err)
		}
		if reply.ID != 271 || !reply.Response || !reply.RecursionAvailable || reply.RCode != test.code || len(reply.Answers) != test.want {
			t.Fatalf("%+v", reply)
		}
	}
	for _, malformed := range [][]byte{nil, {1, 2, 3}, make([]byte, 5000)} {
		if answerDNS(context.Background(), malformed, nil) != nil {
			t.Fatal("accepted malformed DNS")
		}
	}
}

func TestVPNFamilyFallbackStaysWithinDNSAnswer(t *testing.T) {
	r := NewResolver(nil)
	v4, v6 := net.ParseIP("142.250.74.206"), net.ParseIP("2a00:1450:4001:80f::200e")
	r.cache["youtube.com"] = dnsEntry{IPs: []net.IP{v4, v6}, Until: time.Now().Add(time.Minute)}
	if len(r.candidates("youtube.com", v6)) != 3 {
		t.Fatal("lost IPv4 fallback")
	}
	if len(r.candidates("other.example", v6)) != 1 {
		t.Fatal("changed ECH destination")
	}
	if len(r.candidates("youtube.com", net.ParseIP("8.8.8.8"))) != 1 {
		t.Fatal("replaced a destination outside the DNS answer")
	}
}

func TestVPNSettingsValidation(t *testing.T) {
	for _, provider := range []string{"auto", "google", "cloudflare"} {
		r := NewResolver(nil)
		if err := r.selectProvider(provider); err != nil {
			t.Fatal(err)
		}
		for _, p := range r.providers {
			if provider == "google" && p.Host != "dns.google" || provider == "cloudflare" && p.Host != "cloudflare-dns.com" {
				t.Fatal("wrong DNS provider")
			}
		}
	}
	if NewResolver(nil).selectProvider("unexpected") == nil {
		t.Fatal("accepted unknown provider")
	}
	if _, err := Start(Options{Direct: true, Strategy: "unknown"}); err == nil {
		t.Fatal("accepted unknown strategy")
	}
	e, err := Start(Options{Direct: true, Strategy: "tlsrec-sni"})
	if err != nil { // Do not couple test to a particular builtin preset ID.
		cfg, _ := ParseConfig(BuiltinJSON)
		e, err = Start(Options{Direct: true, Strategy: cfg.Strategies[0].ID})
	}
	if err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	id := e.Snapshot().StrategyID
	e.Next()
	if e.Snapshot().State != "blocked" || e.Snapshot().StrategyID != id {
		t.Fatal("manual choice silently switched")
	}
}

func TestSnapshotDetectsExitedEngine(t *testing.T) {
	done := make(chan struct{})
	close(done)
	e := &Engine{child: &childProcess{done: done}, status: Status{State: "ready", Verified: true}}
	if s := e.Snapshot(); s.State != "blocked" || s.Verified || s.Error == "" {
		t.Fatalf("%+v", s)
	}
	e.Snapshot()
	if len(e.events) != 1 {
		t.Fatal("repeated death event")
	}
}
