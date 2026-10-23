// Package mobile предоставляет простой API для gomobile без типов Android.
package mobile

import (
	"encoding/json"
	"errors"
	core "github.com/sonjiwu2/youtube-plugin-android/core"
	"sync"
)

type Engine struct {
	mu     sync.Mutex
	inner  *core.Engine
	tunnel *vpnTunnel
}

func NewEngine() *Engine { return &Engine{} }
func (e *Engine) Start(executable, fakePath, dataDir, profile, remoteURL, publicKey string) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inner != nil {
		return 0, errors.New("engine already running")
	}
	inner, err := core.Start(core.Options{Executable: executable, FakePath: fakePath, DataDir: dataDir, Profile: profile, RemoteURL: remoteURL, PublicKey: publicKey, AutoProbe: true, ParentWatch: true})
	if err != nil {
		return 0, err
	}
	e.inner = inner
	return inner.Port(), nil
}
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inner != nil {
		e.inner.Stop()
		e.tunnel.stop()
		e.tunnel = nil
		e.inner = nil
	}
}

func (e *Engine) StartVPN(fd int, executable, fakePath, dataDir, profile, remoteURL, publicKey, strategy, dns string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inner != nil {
		return errors.New("engine already running")
	}
	inner, err := core.Start(core.Options{Executable: executable, FakePath: fakePath, DataDir: dataDir, Profile: profile, RemoteURL: remoteURL, PublicKey: publicKey, AutoProbe: true, Strategy: strategy, DNS: dns, ParentWatch: true, VPN: true})
	if err != nil {
		return err
	}
	tunnel, err := startTunnel(fd, inner)
	if err != nil {
		inner.Stop()
		return err
	}
	e.inner, e.tunnel = inner, tunnel
	return nil
}

func (e *Engine) Strategies() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inner != nil {
		return e.inner.StrategiesJSON()
	}
	cfg, _ := core.ParseConfig(core.BuiltinJSON)
	b, _ := json.Marshal(cfg.Strategies)
	return string(b)
}
func (e *Engine) Status() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inner == nil {
		return `{"state":"stopped"}`
	}
	return e.inner.StatusJSON()
}
func (e *Engine) Next() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inner != nil {
		e.inner.Next()
	}
}

func (e *Engine) Measure(address string) string {
	e.mu.Lock()
	inner := e.inner
	e.mu.Unlock()
	if inner == nil {
		return `{"Error":"Движок остановлен"}`
	}
	return inner.Measure(address)
}

func (e *Engine) Diagnostics() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inner == nil {
		return "Движок остановлен"
	}
	return e.inner.Diagnostics()
}
