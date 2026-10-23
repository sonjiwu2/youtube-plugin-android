package core

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const firstChunk = 256 * 1024
const minimumSpeed = 96 * 1024

type Options struct {
	Executable, FakePath, DataDir, Profile, RemoteURL, PublicKey string
	Direct                                                       bool
}
type Status struct {
	State         string  `json:"state"`
	Strategy      string  `json:"strategy"`
	StrategyID    string  `json:"strategyId"`
	Speed         float64 `json:"bytesPerSecond"`
	Total         int64   `json:"totalBytes"`
	Attempts      int     `json:"attempts"`
	ConfigVersion int64   `json:"configVersion"`
	Update        string  `json:"update"`
	Error         string  `json:"error"`
}
type Engine struct {
	mu         sync.Mutex
	switchMu   sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	opts       Options
	config     Config
	resolver   *Resolver
	listener   net.Listener
	child      *childProcess
	conns      map[net.Conn]bool
	wg         sync.WaitGroup
	sem        chan struct{}
	index, bad int
	tried      map[string]bool
	status     Status
}

func Start(opts Options) (*Engine, error) {
	cfg, err := ParseConfig(BuiltinJSON)
	if err != nil {
		return nil, err
	}
	if !opts.Direct && opts.Executable == "" {
		return nil, errors.New("ciadpi executable required")
	}
	if opts.DataDir != "" {
		if err = os.MkdirAll(opts.DataDir, 0700); err != nil {
			return nil, err
		}
	}
	// Подписанный кэш проверяется заново при каждом запуске.
	if opts.PublicKey != "" {
		if data, e := os.ReadFile(filepath.Join(opts.DataDir, "remote.json")); e == nil {
			sig, _ := os.ReadFile(filepath.Join(opts.DataDir, "remote.sig"))
			if remote, e := VerifyConfig(data, sig, opts.PublicKey, cfg.Version, time.Now()); e == nil {
				cfg = mergeConfig(cfg, remote)
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{opts: opts, config: cfg, ctx: ctx, cancel: cancel, conns: map[net.Conn]bool{}, sem: make(chan struct{}, 128), tried: map[string]bool{}}
	e.resolver = NewResolver(cfg.Fallback)
	e.index = e.cachedIndex()
	e.status = Status{State: "connecting", ConfigVersion: cfg.Version}
	if !opts.Direct {
		e.child, err = startChild(ctx, opts.Executable, opts.FakePath, cfg.Strategies[e.index])
		if err != nil {
			cancel()
			return nil, err
		}
	}
	// Bootstrap тоже идёт через desync: DoH по имени может фильтроваться тем же DPI.
	e.resolver.dialBootstrap = func(ctx context.Context, address string) (net.Conn, error) {
		e.mu.Lock()
		p := e.child
		e.mu.Unlock()
		if opts.Direct {
			return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(address, "443"))
		}
		if p == nil {
			return nil, errors.New("DoH engine unavailable")
		}
		return dialSOCKS(ctx, p.addr, net.ParseIP(address), 443)
	}
	e.status.Strategy = cfg.Strategies[e.index].Name
	e.status.StrategyID = cfg.Strategies[e.index].ID
	e.tried[e.status.StrategyID] = true
	e.status.Attempts = 1
	e.listener, err = net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		cancel()
		e.child.stop()
		return nil, err
	}
	e.wg.Add(1)
	go e.accept()
	if opts.RemoteURL != "" && opts.PublicKey != "" {
		e.wg.Add(1)
		go func() { defer e.wg.Done(); e.updateLoop() }()
	}
	return e, nil
}

func (e *Engine) Address() string    { return e.listener.Addr().String() }
func (e *Engine) Snapshot() Status   { e.mu.Lock(); defer e.mu.Unlock(); return e.status }
func (e *Engine) StatusJSON() string { b, _ := json.Marshal(e.Snapshot()); return string(b) }
func (e *Engine) Stop() {
	e.cancel()
	e.listener.Close()
	e.switchMu.Lock()
	e.mu.Lock()
	for c := range e.conns {
		c.Close()
	}
	p := e.child
	e.child = nil
	e.status.State = "stopped"
	e.mu.Unlock()
	p.stop()
	e.switchMu.Unlock()
	e.wg.Wait()
}

func (e *Engine) accept() {
	defer e.wg.Done()
	for {
		c, err := e.listener.Accept()
		if err != nil {
			return
		}
		select {
		case e.sem <- struct{}{}:
		default:
			c.Close()
			continue
		}
		e.mu.Lock()
		e.conns[c] = true
		e.mu.Unlock()
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			defer func() { <-e.sem; c.Close(); e.mu.Lock(); delete(e.conns, c); e.mu.Unlock() }()
			e.handle(c)
		}()
	}
}

func (e *Engine) handle(c net.Conn) {
	c.SetDeadline(time.Now().Add(12 * time.Second))
	r := bufio.NewReaderSize(io.LimitReader(c, 16384), 16384)
	req, err := http.ReadRequest(r)
	if err != nil {
		return
	}
	if req.Body != nil {
		req.Body.Close()
	}
	host, port, err := net.SplitHostPort(req.Host)
	if req.Method != "CONNECT" || err != nil || port != "443" || req.ContentLength > 0 || len(req.TransferEncoding) > 0 {
		io.WriteString(c, "HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n")
		return
	}
	host = normalizeHost(host)
	if !ValidHost(host) {
		io.WriteString(c, "HTTP/1.1 403 Forbidden\r\n\r\n")
		return
	}
	target := MatchHost(host, DefaultHosts)
	adaptive := MatchHost(host, []string{"youtube.com", "googlevideo.com", "youtubei.googleapis.com"})
	e.mu.Lock()
	id := e.status.StrategyID
	blocked := e.status.State == "blocked"
	e.mu.Unlock()
	if blocked {
		io.WriteString(c, "HTTP/1.1 503 Service Unavailable\r\n\r\n")
		return
	}
	ips, err := e.resolver.Resolve(e.ctx, host)
	if err != nil {
		io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		e.noteError(id, "DoH недоступен")
		e.failure(id, adaptive)
		return
	}
	if _, err = io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	// ReadRequest мог прочесть часть TLS заранее; после заголовков снимаем лимит.
	buffered := make([]byte, r.Buffered())
	io.ReadFull(r, buffered)
	hello, sni, err := ReadClientHello(io.MultiReader(strings.NewReader(string(buffered)), c))
	if err != nil {
		e.noteError(id, "ClientHello: "+err.Error())
		return
	}
	if sni != host {
		e.noteError(id, "SNI не совпадает с CONNECT")
		return
	}
	var upstream net.Conn
	ctx, cancel := context.WithTimeout(e.ctx, 10*time.Second)
	defer cancel()
	e.mu.Lock()
	p := e.child
	e.mu.Unlock()
	upstream, err = dialCandidates(ctx, ips, func(ctx context.Context, ip net.IP) (net.Conn, error) {
		if target && !e.opts.Direct {
			if p == nil {
				return nil, errors.New("ciadpi unavailable")
			}
			return dialSOCKS(ctx, p.addr, ip, 443)
		} else {
			return (&net.Dialer{Timeout: 4 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), "443"))
		}
	})
	if upstream == nil || err != nil {
		if err != nil {
			e.noteError(id, "Соединение с сервером: "+err.Error())
		}
		e.failure(id, adaptive)
		return
	}
	e.mu.Lock()
	if e.ctx.Err() != nil {
		e.mu.Unlock()
		upstream.Close()
		return
	}
	e.conns[upstream] = true
	e.mu.Unlock()
	defer func() { upstream.Close(); e.mu.Lock(); delete(e.conns, upstream); e.mu.Unlock() }()
	upstream.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err = upstream.Write(hello); err != nil {
		e.failure(id, adaptive)
		return
	}
	upstream.SetWriteDeadline(time.Time{})
	c.SetDeadline(time.Time{})
	done := make(chan struct{})
	clientEnded := make(chan struct{}, 1)
	go func() { io.Copy(upstream, c); clientEnded <- struct{}{}; upstream.Close(); close(done) }()
	bytes, speed, readErr := e.copyDown(c, upstream, MatchHost(host, []string{"googlevideo.com"}), id)
	clientCancelled := false
	select {
	case <-clientEnded:
		clientCancelled = true
	default:
	}
	if bytes == 0 && readErr != nil {
		e.noteError(id, "Нет ответа TLS: "+readErr.Error())
	}
	c.Close()
	upstream.Close()
	<-done
	if adaptive && !clientCancelled && bytes < 1024 && readErr != nil {
		e.failure(id, true)
	} else if target && bytes >= 1024 {
		e.mu.Lock()
		if e.status.StrategyID == id && e.status.State != "blocked" {
			e.status.State = "ready"
			e.status.Error = ""
		}
		e.mu.Unlock()
	}
	if MatchHost(host, []string{"googlevideo.com"}) {
		if bytes >= 32768 && speed > 0 {
			e.sample(id, speed)
		} else if bytes < 32768 && !clientCancelled {
			if n, ok := readErr.(net.Error); ok && n.Timeout() {
				e.failure(id, true)
			}
		}
	}
}

func (e *Engine) noteError(id, message string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.status.StrategyID == id && e.status.State != "blocked" {
		e.status.Error = message
	}
}

func (e *Engine) copyDown(dst, src net.Conn, video bool, id string) (int64, float64, error) {
	buf := make([]byte, 32768)
	var total int64
	var start time.Time
	var speed float64
	sampled := false
	for {
		src.SetReadDeadline(time.Now().Add(18 * time.Second))
		n, err := src.Read(buf)
		if n > 0 {
			if start.IsZero() {
				start = time.Now()
			}
			total += int64(n)
			dst.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, w := dst.Write(buf[:n]); w != nil {
				return total, speed, w
			}
			e.mu.Lock()
			e.status.Total += int64(n)
			if total >= 1024 && e.status.StrategyID == id && e.status.State != "blocked" {
				e.status.State = "ready"
			}
			if video && total >= 32768 && time.Since(start) > 300*time.Millisecond {
				speed = float64(total) / time.Since(start).Seconds()
				e.status.Speed = speed
			}
			e.mu.Unlock()
			if video && !sampled && total >= firstChunk {
				sampled = true
				if speed == 0 {
					speed = float64(total) / time.Since(start).Seconds()
				}
				e.sample(id, speed)
			}
		}
		if err != nil {
			if sampled {
				speed = 0
			}
			return total, speed, err
		}
	}
}
func (e *Engine) failure(id string, target bool) {
	if !target {
		return
	}
	e.mu.Lock()
	if id != e.status.StrategyID || e.ctx.Err() != nil {
		e.mu.Unlock()
		return
	}
	e.bad++
	change := e.bad >= 2
	e.mu.Unlock()
	if change {
		e.next(id)
	}
}
func (e *Engine) sample(id string, speed float64) {
	if speed < minimumSpeed {
		e.failure(id, true)
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if id == e.status.StrategyID {
		e.bad = 0
		e.status.State = "ready"
		e.saveStrategyLocked(id)
	}
}

func (e *Engine) Next() { e.mu.Lock(); id := e.status.StrategyID; e.mu.Unlock(); e.next(id) }
func (e *Engine) next(expected string) {
	e.switchMu.Lock()
	defer e.switchMu.Unlock()
	e.mu.Lock()
	if e.ctx.Err() != nil || e.status.StrategyID != expected {
		e.mu.Unlock()
		return
	}
	next := -1
	for i := range e.config.Strategies {
		if !e.tried[e.config.Strategies[i].ID] {
			next = i
			break
		}
	}
	if next < 0 {
		e.status.State = "blocked"
		e.status.Error = "Все встроенные стратегии проверены. В этой сети обход сейчас недоступен."
		for c := range e.conns {
			c.Close()
		}
		old := e.child
		e.child = nil
		e.mu.Unlock()
		old.stop()
		return
	}
	s := e.config.Strategies[next]
	e.tried[s.ID] = true
	e.status.State = "selecting"
	e.status.Attempts++
	e.mu.Unlock()
	var p *childProcess
	var err error
	if !e.opts.Direct {
		p, err = startChild(e.ctx, e.opts.Executable, e.opts.FakePath, s)
	}
	e.mu.Lock()
	old := e.child
	e.child = p
	e.index = next
	e.bad = 0
	e.status.StrategyID = s.ID
	e.status.Strategy = s.Name
	e.status.Speed = 0
	if err != nil {
		e.status.Error = "Не удалось запустить стратегию"
	} else {
		e.status.Error = ""
	}
	for c := range e.conns {
		c.Close()
	}
	e.mu.Unlock()
	old.stop()
	if err != nil {
		e.mu.Lock()
		e.bad = 1
		e.mu.Unlock()
	}
}

func mergeConfig(base, remote Config) Config {
	seen := map[string]bool{}
	for _, s := range remote.Strategies {
		seen[s.ID] = true
	}
	for _, s := range base.Strategies {
		if !seen[s.ID] {
			remote.Strategies = append(remote.Strategies, s)
		}
	}
	if remote.Fallback == nil {
		remote.Fallback = base.Fallback
	}
	return remote
}

// Port возвращается числом для Java API gomobile.
func (e *Engine) Port() int {
	_, p, _ := net.SplitHostPort(e.Address())
	n, _ := strconv.Atoi(p)
	return n
}
