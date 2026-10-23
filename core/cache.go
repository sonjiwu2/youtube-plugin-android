package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

func (e *Engine) cachePath() string {
	sum := sha256.Sum256([]byte(e.opts.Profile))
	return filepath.Join(e.opts.DataDir, "network-"+hex.EncodeToString(sum[:12])+".json")
}
func (e *Engine) cachedIndex() int {
	var saved struct {
		ID string
		At int64
	}
	b, _ := os.ReadFile(e.cachePath())
	json.Unmarshal(b, &saved)
	if time.Since(time.Unix(saved.At, 0)) > 7*24*time.Hour {
		return 0
	}
	for i, s := range e.config.Strategies {
		if s.ID == saved.ID {
			return i
		}
	}
	return 0
}
func (e *Engine) saveStrategyLocked(id string) {
	if e.opts.DataDir == "" {
		return
	}
	b, _ := json.Marshal(struct {
		ID string
		At int64
	}{id, time.Now().Unix()})
	_ = atomicWrite(e.cachePath(), b)
}
func atomicWrite(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
