package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestExplicitCheckReportsBothEndpointsWithoutChangingPresetOrTraffic(t *testing.T) {
	e, err := Start(Options{Direct: true, VPN: true, Strategy: "disorder-tlsrec"})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	before := e.Snapshot()
	var hosts []string
	report := e.checkConnectivity(func(_ context.Context, _ *childProcess, host string) error {
		hosts = append(hosts, host)
		if host == "m.youtube.com" {
			return errors.New("homepage blocked")
		}
		return nil
	})
	var result struct {
		Checks  []endpointCheck `json:"checks"`
		Changed bool            `json:"strategyChanged"`
	}
	if json.Unmarshal([]byte(report), &result) != nil || len(result.Checks) != 2 || len(hosts) != 2 {
		t.Fatal(report)
	}
	if result.Checks[0].OK || !result.Checks[1].OK || result.Changed {
		t.Fatal(report)
	}
	after := e.Snapshot()
	if after.StrategyID != before.StrategyID || after.Attempts != 1 || after.Total != 0 || after.VideoBytes != 0 || after.Verified {
		t.Fatal("diagnostics changed selection or fabricated traffic", after)
	}
}

func TestTLSOnlyConnectionsDoNotPretendToBeVideoData(t *testing.T) {
	e, err := Start(Options{Direct: true, VPN: true})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	id := e.Snapshot().StrategyID
	for range 40 {
		src := &deadlineStream{input: bytes.NewReader(make([]byte, 8192))}
		e.copyDown(&deadlineStream{}, src, true, id, true)
	}
	if s := e.Snapshot(); s.VideoBytes != 0 || s.VideoStreams != 0 || s.Total == 0 {
		t.Fatal("TLS handshakes were counted as video", s)
	}
	src := &deadlineStream{input: bytes.NewReader(make([]byte, 40960))}
	e.copyDown(&deadlineStream{}, src, true, id, true)
	if s := e.Snapshot(); s.VideoBytes != 8192 || s.VideoStreams != 1 {
		t.Fatal("substantial CDN data was not counted", s)
	}
}
