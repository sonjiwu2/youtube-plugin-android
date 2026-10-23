package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

type Benchmark struct {
	Before, After float64
	Error         string
}

// Measure сравнивает один и тот же видеофрагмент, не сохраняя подписанный URL.
// Прямой запрос здесь нужен только для явного пользовательского замера «до».
func (e *Engine) Measure(address string) string {
	result := Benchmark{}
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.User != nil || !MatchHost(u.Hostname(), []string{"googlevideo.com"}) || u.Port() != "" && u.Port() != "443" || len(address) > 16384 {
		result.Error = "Сначала запустите видео"
		b, _ := json.Marshal(result)
		return string(b)
	}
	measure := func(direct bool) (float64, error) {
		ctx, cancel := context.WithTimeout(e.ctx, 10*time.Second)
		defer cancel()
		tr := &http.Transport{TLSHandshakeTimeout: 6 * time.Second, DisableKeepAlives: true}
		if direct {
			tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
				ips, err := e.resolver.Resolve(ctx, u.Hostname())
				if err != nil {
					return nil, err
				}
				return dialCandidates(ctx, ips, func(ctx context.Context, ip net.IP) (net.Conn, error) {
					return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), "443"))
				})
			}
		} else {
			proxy, _ := url.Parse("http://" + e.Address())
			tr.Proxy = http.ProxyURL(proxy)
		}
		defer tr.CloseIdleConnections()
		client := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Range", "bytes=0-262143")
		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 && resp.StatusCode != 206 {
			return 0, errors.New("video fragment unavailable")
		}
		n, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, firstChunk))
		if n < 16384 {
			return 0, errors.New("not enough video data")
		}
		return float64(n) / time.Since(start).Seconds(), readErr
	}
	result.Before, _ = measure(true)
	if e.ctx.Err() == nil {
		result.After, err = measure(false)
	}
	if err != nil || result.After == 0 {
		result.Error = "Фрагмент недоступен. Запустите видео и повторите замер."
	}
	b, _ := json.Marshal(result)
	return string(b)
}
