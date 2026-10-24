package core

import (
	"context"
	"encoding/json"
	"time"
)

type endpointCheck struct {
	Host      string `json:"host"`
	OK        bool   `json:"ok"`
	ElapsedMS int64  `json:"elapsedMs"`
	Error     string `json:"error,omitempty"`
}

// CheckConnectivity is explicit diagnostics, not strategy selection. Its own
// requests never increment the per-app VPN traffic counters or reset playback.
func (e *Engine) CheckConnectivity() string { return e.checkConnectivity(e.probeEndpoint) }

func (e *Engine) checkConnectivity(check func(context.Context, *childProcess, string) error) string {
	e.mu.Lock()
	id, child := e.status.StrategyID, e.child
	e.mu.Unlock()
	ctx, cancel := context.WithTimeout(e.ctx, 22*time.Second)
	defer cancel()
	checks := []endpointCheck{}
	for _, host := range []string{"m.youtube.com", "youtubei.googleapis.com"} {
		start := time.Now()
		request, done := context.WithTimeout(ctx, 10*time.Second)
		err := check(request, child, host)
		done()
		result := endpointCheck{Host: host, OK: err == nil, ElapsedMS: time.Since(start).Milliseconds()}
		if err != nil {
			result.Error = err.Error()
		}
		checks = append(checks, result)
	}
	after := e.Snapshot()
	b, _ := json.Marshal(struct {
		Checks   []endpointCheck `json:"checks"`
		Strategy string          `json:"strategyId"`
		Changed  bool            `json:"strategyChanged"`
	}{checks, id, after.StrategyID != id || e.ctx.Err() != nil})
	return string(b)
}
