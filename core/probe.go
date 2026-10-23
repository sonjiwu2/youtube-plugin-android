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
	"strings"
	"time"
)

type diagnosticEvent struct {
	At       string `json:"at"`
	Strategy string `json:"strategy"`
	Message  string `json:"message"`
}

type probedConn struct {
	*tls.Conn
	ip net.IP
}

func (e *Engine) eventLocked(id, message string) {
	if len(message) > 700 {
		message = message[:700]
	}
	e.events = append(e.events, diagnosticEvent{time.Now().UTC().Format(time.RFC3339), id, message})
	if len(e.events) > 80 {
		e.events = e.events[len(e.events)-80:]
	}
}

// Отчёт не содержит cookies, URL видео, профиля сети или содержимого запросов.
func (e *Engine) Diagnostics() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	b, _ := json.MarshalIndent(struct {
		Status Status            `json:"status"`
		Events []diagnosticEvent `json:"events"`
	}{e.status, e.events}, "", "  ")
	return string(b)
}

func (e *Engine) wakeProbe() {
	select {
	case e.probeWake <- struct{}{}:
	default:
	}
}

// Только проверенный HTTPS-ответ YouTube открывает браузер. Ошибка отдельного
// ресурса или несколько байт TLS не являются доказательством рабочего обхода.
func (e *Engine) probeLoop(check func(context.Context, *childProcess) error) {
	defer e.wg.Done()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-e.probeWake:
		}
		e.mu.Lock()
		id, p := e.status.StrategyID, e.child
		ctx, cancel := context.WithTimeout(e.ctx, 12*time.Second)
		e.probeCancel = cancel
		e.eventLocked(id, "Проверка HTTPS m.youtube.com")
		e.mu.Unlock()
		err := check(ctx, p)
		cancel()
		e.mu.Lock()
		if e.ctx.Err() != nil || e.status.StrategyID != id {
			e.mu.Unlock()
			continue
		}
		e.probeCancel = nil
		if err == nil {
			e.status.Verified = true
			e.status.State = "ready"
			e.status.Error = ""
			e.bad = 0
			e.eventLocked(id, "YouTube: HTTPS 200, HTML получен")
			e.saveStrategyLocked(id)
			e.mu.Unlock()
		} else {
			e.status.Error = "Проверка YouTube: " + err.Error()
			e.eventLocked(id, e.status.Error)
			if p != nil && p.output.String() != "" {
				e.eventLocked(id, "ciadpi: "+p.output.String())
			}
			e.mu.Unlock()
			if e.resolver != nil {
				e.resolver.retryAfterFailure("m.youtube.com")
			}
			e.next(id)
		}
	}
}

func (e *Engine) probeYouTube(ctx context.Context, p *childProcess) error {
	if !e.opts.Direct && p == nil {
		return errors.New("движок недоступен")
	}
	ips, err := e.resolver.Resolve(ctx, "m.youtube.com")
	if err != nil {
		return err
	}
	// Соревнуются полные TLS handshakes, а не только TCP: доступный IPv6
	// маршрут ещё не означает, что на нём проходит HTTPS к YouTube.
	tr := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 5 * time.Second,
		DialTLSContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			c, err := dialCandidates(ctx, ips, func(ctx context.Context, ip net.IP) (net.Conn, error) {
				var c net.Conn
				var err error
				if e.opts.Direct {
					c, err = (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), "443"))
				} else {
					c, err = dialSOCKS(ctx, p.addr, ip, 443)
				}
				if err != nil {
					return nil, err
				}
				t := tls.Client(c, &tls.Config{ServerName: "m.youtube.com", MinVersion: tls.VersionTLS12})
				if err = t.HandshakeContext(ctx); err != nil {
					c.Close()
					return nil, err
				}
				return &probedConn{t, ip}, nil
			})
			if err != nil {
				return nil, err
			}
			winner := c.(*probedConn)
			e.resolver.prefer("m.youtube.com", winner.ip)
			return winner.Conn, nil
		}}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://m.youtube.com/", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return validateYouTubeResponse(resp)
}

func validateYouTubeResponse(resp *http.Response) error {
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		return errors.New("вместо страницы получен другой ответ")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 65536))
	if err != nil {
		return err
	}
	if len(body) < 1024 || !strings.Contains(strings.ToLower(string(body)), "youtube") {
		return errors.New("неполная страница YouTube")
	}
	return nil
}
