package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"time"
)

func (e *Engine) updateLoop() {
	e.updateRemote()
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			e.updateRemote()
		}
	}
}
func (e *Engine) updateRemote() {
	fail := func() {
		e.mu.Lock()
		e.status.Update = "Встроенные стратегии · обновление недоступно"
		e.mu.Unlock()
	}
	u, err := url.Parse(e.opts.RemoteURL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		fail()
		return
	}
	proxy, _ := url.Parse("http://" + e.Address())
	tr := &http.Transport{Proxy: http.ProxyURL(proxy), TLSHandshakeTimeout: 8 * time.Second}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 20 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 5 || r.URL.Scheme != "https" {
			return errors.New("invalid redirect")
		}
		return nil
	}}
	fetch := func(address string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(e.ctx, 20*time.Second)
		defer cancel()
		req, er := http.NewRequestWithContext(ctx, "GET", address, nil)
		if er != nil {
			return nil, er
		}
		resp, er := client.Do(req)
		if er != nil {
			return nil, er
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, errors.New("update HTTP error")
		}
		data, er := io.ReadAll(io.LimitReader(resp.Body, 256*1024+1))
		if er != nil || len(data) > 256*1024 {
			return nil, errors.New("update too large")
		}
		return data, nil
	}
	data, err := fetch(u.String())
	if err != nil {
		fail()
		return
	}
	sig, err := fetch(u.String() + ".sig")
	if err != nil {
		fail()
		return
	}
	e.mu.Lock()
	minimum := e.config.Version
	e.mu.Unlock()
	cfg, err := VerifyConfig(data, sig, e.opts.PublicKey, minimum, time.Now())
	if err != nil {
		fail()
		return
	}
	if err = atomicWrite(filepath.Join(e.opts.DataDir, "remote.json"), data); err != nil {
		fail()
		return
	}
	if err = atomicWrite(filepath.Join(e.opts.DataDir, "remote.sig"), sig); err != nil {
		fail()
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if cfg.Version > e.config.Version {
		e.config = mergeConfig(e.config, cfg)
		e.status.ConfigVersion = cfg.Version
		e.status.Update = "Подписанные стратегии обновлены"
	} else {
		e.status.Update = "Стратегии актуальны"
	}
}
