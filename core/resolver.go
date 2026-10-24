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
	"sort"
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
	preferred     string
	sources       map[string]string
	rejected      map[string]string
	fallback      map[string][]string
	inflight      map[string]chan struct{}
	lastErrors    map[string]string
	transports    map[string]*http.Transport
	dialBootstrap func(context.Context, string) (net.Conn, error)
}

func NewResolver(fallback map[string][]string) *Resolver {
	return &Resolver{cache: map[string]dnsEntry{}, sources: map[string]string{}, inflight: map[string]chan struct{}{}, fallback: fallback, providers: []dohProvider{
		{"dns.google", "8.8.8.8", "/resolve"}, {"cloudflare-dns.com", "1.1.1.1", "/dns-query"}, {"dns.google", "8.8.4.4", "/resolve"}, {"cloudflare-dns.com", "1.0.0.1", "/dns-query"},
		{"dns.google", "2001:4860:4860::8888", "/resolve"}, {"cloudflare-dns.com", "2606:4700:4700::1111", "/dns-query"},
	}}
}

func (r *Resolver) selectProvider(provider string) error {
	if provider == "" || provider == "auto" {
		return nil
	}
	host := map[string]string{"google": "dns.google", "cloudflare": "cloudflare-dns.com"}[provider]
	if host == "" {
		return errors.New("unknown DNS provider")
	}
	filtered := r.providers[:0]
	for _, p := range r.providers {
		if p.Host == host {
			filtered = append(filtered, p)
		}
	}
	r.providers = filtered
	return nil
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
	r.mu.Lock()
	providers := append([]dohProvider{}, r.providers...)
	if bad := r.rejected[host]; bad != "" {
		var alternatives []dohProvider
		for _, p := range providers {
			if p.Host != bad {
				alternatives = append(alternatives, p)
			}
		}
		if len(alternatives) > 0 {
			providers = alternatives
		}
	}
	preferred := r.preferred
	if preferred == "" && len(providers) > 0 {
		preferred = providers[0].Host
	}
	r.mu.Unlock()
	type sourcedResult struct {
		result
		source string
	}
	answers := make(chan sourcedResult, len(providers))
	for _, p := range providers {
		go func(p dohProvider) {
			delay := time.Duration(0)
			if p.Host != preferred {
				delay += 450 * time.Millisecond
			}
			if net.ParseIP(p.IP).To4() == nil {
				delay += 600 * time.Millisecond
			}
			if delay > 0 {
				timer := time.NewTimer(delay)
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-ctx.Done():
					return
				}
			}
			ips, ttl, _ := r.query(ctx, p, host)
			answers <- sourcedResult{result{ips, ttl}, p.Host}
		}(p)
	}
	for range providers {
		select {
		case a := <-answers:
			if len(a.ips) > 0 {
				r.mu.Lock()
				if len(r.cache) > 2048 {
					clear(r.cache)
				}
				r.cache[host] = dnsEntry{a.ips, time.Now().Add(a.ttl)}
				if r.sources == nil {
					r.sources = map[string]string{}
				}
				if len(r.sources) > 2048 {
					clear(r.sources)
				}
				r.sources[host] = a.source
				if r.preferred == "" {
					r.preferred = a.source
				}
				r.mu.Unlock()
				return a.ips, nil
			}
		case <-ctx.Done():
			return r.fallbackFor(host)
		}
	}
	return r.fallbackFor(host)
}

// An answer can be authentic but contain unreachable regional frontends. In
// automatic mode a failed connection must try the other encrypted resolver,
// rather than exhausting every DPI preset against the same dead addresses.
func (r *Resolver) retryAfterFailure(host string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	source := r.sources[normalizeHost(host)]
	if source == "" {
		source = r.preferred
	}
	if source == "" && len(r.providers) > 0 {
		source = r.providers[0].Host
	}
	if r.rejected == nil {
		r.rejected = map[string]string{}
	}
	if len(r.rejected) > 2048 {
		clear(r.rejected)
	}
	r.rejected[normalizeHost(host)] = source
	if r.preferred == "" || r.preferred == source {
		for _, p := range r.providers {
			if p.Host != source {
				r.preferred = p.Host
				break
			}
		}
	}
	clear(r.cache)
}

func (r *Resolver) destinationCandidates(destination net.IP) (string, []net.IP) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for host, entry := range r.cache {
		if !MatchHost(host, DefaultHosts) || !time.Now().Before(entry.Until) {
			continue
		}
		for _, ip := range entry.IPs {
			if ip.Equal(destination) {
				return host, append([]net.IP{destination}, entry.IPs...)
			}
		}
	}
	return "", []net.IP{destination}
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
		if r.sources != nil {
			r.sources[host] = r.preferred
		}
		r.mu.Unlock()
		return nil, fmt.Errorf("DoH unavailable for %s", host)
	}
	r.mu.Lock()
	if len(r.cache) > 2048 {
		clear(r.cache)
	}
	r.cache[host] = dnsEntry{IPs: ips, Until: time.Now().Add(30 * time.Second)}
	if r.sources != nil {
		r.sources[host] = r.preferred
	}
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
	// У части мобильных сетей IPv6 объявлен, но недоступен; IPv4 получает небольшой приоритет.
	sort.SliceStable(ips, func(i, j int) bool { return ips[i].To4() != nil && ips[j].To4() == nil })
	return ips, ttl, nil
}

func (r *Resolver) queryType(ctx context.Context, p dohProvider, host, kind string) ([]net.IP, time.Duration, error) {
	if r.dialBootstrap == nil {
		return r.queryTypeRoute(ctx, p, host, kind, false)
	}
	// Доступ к DoH не должен зависеть исключительно от ещё не подобранной
	// стратегии YouTube. Оба пути используют pinned IP и проверку сертификата.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		ips []net.IP
		ttl time.Duration
		err error
	}
	results := make(chan result, 2)
	for _, direct := range []bool{false, true} {
		go func(direct bool) {
			if direct {
				timer := time.NewTimer(200 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-ctx.Done():
					results <- result{err: ctx.Err()}
					return
				}
			}
			ips, ttl, err := r.queryTypeRoute(ctx, p, host, kind, direct)
			results <- result{ips, ttl, err}
		}(direct)
	}
	var last error
	for range 2 {
		select {
		case a := <-results:
			if len(a.ips) > 0 {
				return a.ips, a.ttl, nil
			}
			last = a.err
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		}
	}
	return nil, 0, last
}

func (r *Resolver) prefer(host string, ip net.IP) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.cache[host]
	ips := []net.IP{append(net.IP(nil), ip...)}
	for _, other := range entry.IPs {
		if !ip.Equal(other) {
			ips = append(ips, other)
		}
	}
	entry.IPs = ips
	r.cache[host] = entry
}

// The TUN handshake terminates locally, so the app cannot use its own TCP
// Happy Eyeballs to discover a broken upstream IPv6 route. Race only addresses
// from the same DNS answer, preserving the original destination for ECH.
func (r *Resolver) candidates(host string, destination net.IP) []net.IP {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.cache[normalizeHost(host)]
	if time.Now().Before(entry.Until) {
		for _, ip := range entry.IPs {
			if ip.Equal(destination) {
				return append([]net.IP{destination}, entry.IPs...)
			}
		}
	}
	return []net.IP{destination}
}

func (r *Resolver) transport(p dohProvider, direct bool) *http.Transport {
	key := fmt.Sprintf("%s/%s/%v", p.Host, p.IP, direct)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.transports == nil {
		r.transports = map[string]*http.Transport{}
	}
	if tr := r.transports[key]; tr != nil {
		return tr
	}
	// Cronet resolves many CDN names at once. Reuse encrypted DNS connections
	// instead of creating up to 24 TCP handshakes per hostname and starving ciadpi.
	tr := &http.Transport{Proxy: nil, ForceAttemptHTTP2: true,
		MaxConnsPerHost: 2, MaxIdleConnsPerHost: 2, IdleConnTimeout: 60 * time.Second,
		TLSHandshakeTimeout: 4 * time.Second, TLSClientConfig: &tls.Config{ServerName: p.Host, MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			if r.dialBootstrap != nil && !direct {
				return r.dialBootstrap(ctx, p.IP)
			}
			return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(p.IP, "443"))
		}}
	r.transports[key] = tr
	return tr
}

func (r *Resolver) closeIdle() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, tr := range r.transports {
		tr.CloseIdleConnections()
	}
}

func (r *Resolver) queryTypeRoute(ctx context.Context, p dohProvider, host, kind string, direct bool) (ips []net.IP, duration time.Duration, failure error) {
	defer func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.lastErrors == nil {
			r.lastErrors = map[string]string{}
		}
		key := fmt.Sprintf("%s %s direct=%v", p.Host, p.IP, direct)
		if failure != nil && !errors.Is(failure, context.Canceled) {
			r.lastErrors[key] = failure.Error()
		}
		if failure == nil {
			delete(r.lastErrors, key)
		}
	}()
	tr := r.transport(p, direct)
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
	ips = nil
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
