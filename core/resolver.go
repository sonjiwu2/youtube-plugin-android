package core

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type dohProvider struct{ Host, IP, Path string }
type dnsEntry struct {
	IPs   []net.IP
	Until time.Time
}
type Resolver struct {
	mu            sync.Mutex
	cache         map[string]dnsEntry
	providers     []dohProvider
	fallback      map[string][]string
	inflight      map[string]chan struct{}
	dialBootstrap func(context.Context, string) (net.Conn, error)
}

func NewResolver(fallback map[string][]string) *Resolver {
	return &Resolver{cache: map[string]dnsEntry{}, inflight: map[string]chan struct{}{}, fallback: fallback, providers: []dohProvider{
		{"dns.google", "8.8.8.8", "/resolve"}, {"cloudflare-dns.com", "1.1.1.1", "/dns-query"}, {"dns.google", "8.8.4.4", "/resolve"}, {"cloudflare-dns.com", "1.0.0.1", "/dns-query"},
		{"dns.google", "2001:4860:4860::8888", "/resolve"}, {"cloudflare-dns.com", "2606:4700:4700::1111", "/dns-query"},
	}}
}

func (r *Resolver) Resolve(ctx context.Context, host string) ([]net.IP, error) {
	host = normalizeHost(host)
	if ip := net.ParseIP(host); ip != nil {
		if publicIP(ip) {
			return []net.IP{ip}, nil
		}
		return nil, errors.New("non-public address")
	}
	if !ValidHost(host) {
		return nil, errors.New("invalid hostname")
	}
	for {
		r.mu.Lock()
		cached := r.cache[host]
		if time.Now().Before(cached.Until) {
			r.mu.Unlock()
			if len(cached.IPs) == 0 {
				return nil, errors.New("DoH temporarily unavailable")
			}
			return cached.IPs, nil
		}
		if pending := r.inflight[host]; pending != nil {
			r.mu.Unlock()
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		r.inflight[host] = make(chan struct{})
		r.mu.Unlock()
		break
	}
	defer func() { r.mu.Lock(); close(r.inflight[host]); delete(r.inflight, host); r.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(ctx, 7*time.Second)
	defer cancel()
	type result struct {
		ips []net.IP
		ttl time.Duration
	}
	answers := make(chan result, len(r.providers))
	for _, p := range r.providers {
		go func(p dohProvider) { ips, ttl, _ := r.query(ctx, p, host); answers <- result{ips, ttl} }(p)
	}
	for range r.providers {
		select {
		case a := <-answers:
			if len(a.ips) > 0 {
				r.mu.Lock()
				if len(r.cache) > 2048 {
					clear(r.cache)
				}
				r.cache[host] = dnsEntry{a.ips, time.Now().Add(a.ttl)}
				r.mu.Unlock()
				return a.ips, nil
			}
		case <-ctx.Done():
			return r.fallbackFor(host)
		}
	}
	return r.fallbackFor(host)
}

func (r *Resolver) fallbackFor(host string) ([]net.IP, error) {
	var ips []net.IP
	// У CDN нет общего IP: запасные адреса разрешены только для точных имён.
	for _, s := range r.fallback[host] {
		if ip := net.ParseIP(s); publicIP(ip) {
			ips = append(ips, ip)
		}
	}
	if len(ips) == 0 {
		r.mu.Lock()
		if len(r.cache) > 2048 {
			clear(r.cache)
		}
		r.cache[host] = dnsEntry{Until: time.Now().Add(10 * time.Second)}
		r.mu.Unlock()
		return nil, fmt.Errorf("DoH unavailable for %s", host)
	}
	r.mu.Lock()
	if len(r.cache) > 2048 {
		clear(r.cache)
	}
	r.cache[host] = dnsEntry{IPs: ips, Until: time.Now().Add(30 * time.Second)}
	r.mu.Unlock()
	return ips, nil
}

func (r *Resolver) query(ctx context.Context, p dohProvider, host string) ([]net.IP, time.Duration, error) {
	type answer struct {
		ips []net.IP
		ttl time.Duration
	}
	answers := make(chan answer, 2)
	for _, kind := range []string{"A", "AAAA"} {
		go func(kind string) { ips, ttl, _ := r.queryType(ctx, p, host, kind); answers <- answer{ips, ttl} }(kind)
	}
	var ips []net.IP
	ttl := 300 * time.Second
	for range 2 {
		select {
		case a := <-answers:
			ips = append(ips, a.ips...)
			if a.ttl > 0 && a.ttl < ttl {
				ttl = a.ttl
			}
		case <-ctx.Done():
			return ips, ttl, ctx.Err()
		}
	}
	return ips, ttl, nil
}

func (r *Resolver) queryType(ctx context.Context, p dohProvider, host, kind string) ([]net.IP, time.Duration, error) {
	tr := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{ServerName: p.Host, MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		if r.dialBootstrap != nil {
			return r.dialBootstrap(ctx, p.IP)
		}
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(p.IP, "443"))
	}}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://"+p.Host+p.Path+"?name="+url.QueryEscape(host)+"&type="+kind, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/dns-json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, 0, errors.New("DoH HTTP error")
	}
	var body struct {
		Status int
		Answer []struct {
			Type int
			TTL  int
			Data string
		}
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&body); err != nil || body.Status != 0 {
		return nil, 0, errors.New("DoH answer error")
	}
	var ips []net.IP
	ttl := 300
	for _, a := range body.Answer {
		if a.Type == 1 || a.Type == 28 {
			if ip := net.ParseIP(a.Data); publicIP(ip) {
				ips = append(ips, ip)
				if a.TTL < ttl {
					ttl = a.TTL
				}
			}
		}
	}
	if ttl < 1 {
		ttl = 1
	}
	return ips, time.Duration(ttl) * time.Second, nil
}
