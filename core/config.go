package core

import (
	"crypto/ed25519"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

//go:embed presets.json
var BuiltinJSON []byte

type Strategy struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Args []string `json:"args"`
}
type Config struct {
	Version    int64               `json:"version"`
	Expires    string              `json:"expires"`
	Strategies []Strategy          `json:"strategies"`
	Fallback   map[string][]string `json:"fallback"`
}

func ParseConfig(data []byte) (Config, error) {
	var c Config
	if len(data) > 256*1024 {
		return c, errors.New("config too large")
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, err
	}
	if c.Version < 1 || len(c.Strategies) < 1 || len(c.Strategies) > 32 {
		return c, errors.New("invalid config")
	}
	seen := map[string]bool{}
	for _, s := range c.Strategies {
		if s.ID == "" || len(s.ID) > 64 || len(s.Name) > 80 || seen[s.ID] || len(s.Args) > 32 {
			return c, errors.New("invalid strategy")
		}
		seen[s.ID] = true
		if err := validateArgs(s.Args); err != nil {
			return c, err
		}
	}
	for host, ips := range c.Fallback {
		if !ValidHost(host) || len(ips) > 8 {
			return c, errors.New("invalid fallback")
		}
		for _, s := range ips {
			if !publicIP(net.ParseIP(s)) {
				return c, errors.New("invalid fallback IP")
			}
		}
	}
	return c, nil
}

// Только параметры desync: удалённый JSON не может менять слушатель, читать файлы или запускать команды.
func validateArgs(args []string) error {
	if len(args)%2 != 0 {
		return errors.New("argument needs value")
	}
	for i := 0; i < len(args); i += 2 {
		flag, v := args[i], args[i+1]
		if len(v) > 80 || strings.ContainsAny(v, "\x00\r\n") {
			return errors.New("invalid argument")
		}
		switch flag {
		case "-s", "-d", "-o", "-q", "-f", "-r":
			for _, ch := range v {
				if !strings.ContainsRune("0123456789-:+shemn", ch) {
					return errors.New("invalid position")
				}
			}
			if v == "" {
				return errors.New("empty position")
			}
		case "-t":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 32 {
				return errors.New("invalid TTL")
			}
		case "-n":
			if !ValidHost(v) {
				return errors.New("invalid fake SNI")
			}
		case "-Q":
			if v != "rand" && v != "orig" {
				return errors.New("invalid TLS mod")
			}
		case "-l":
			if v != "@google" {
				return errors.New("untrusted file")
			}
		default:
			return fmt.Errorf("unsupported strategy option %s", flag)
		}
	}
	return nil
}

func VerifyConfig(data, signature []byte, key string, minVersion int64, now time.Time) (Config, error) {
	var empty Config
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(key))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return empty, errors.New("invalid public key")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(signature)))
	if err != nil || !ed25519.Verify(pub, data, sig) {
		return empty, errors.New("invalid signature")
	}
	c, err := ParseConfig(data)
	if err != nil {
		return empty, err
	}
	expiry, err := time.Parse(time.RFC3339, c.Expires)
	if err != nil || !expiry.After(now) || expiry.After(now.AddDate(1, 0, 0)) || c.Version < minVersion {
		return empty, errors.New("expired or downgraded config")
	}
	return c, nil
}
