package core

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// ServeTCP accepts only connections from the platform's per-app VPN. Outbound
// sockets belong to the plugin UID, which must never be included in that VPN.
func (e *Engine) ServeTCP(c net.Conn, destination net.IP, port uint16) {
	defer c.Close()
	select {
	case e.sem <- struct{}{}:
		defer func() { <-e.sem }()
	default:
		return
	}
	e.mu.Lock()
	if e.ctx.Err() != nil {
		e.mu.Unlock()
		return
	}
	e.wg.Add(1)
	e.conns[c] = true
	e.mu.Unlock()
	defer e.wg.Done()
	defer func() { e.mu.Lock(); delete(e.conns, c); e.mu.Unlock() }()
	c.SetDeadline(time.Now().Add(15 * time.Second))
	if port == 53 {
		e.serveDNSTCP(c)
		return
	}
	if !publicIP(destination) {
		return
	}
	if port == 80 {
		ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
		defer cancel()
		up, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(destination.String(), "80"))
		if err != nil {
			return
		}
		defer up.Close()
		c.SetDeadline(time.Time{})
		up.SetDeadline(time.Now().Add(30 * time.Second))
		done := make(chan struct{})
		go func() { io.Copy(up, c); up.Close(); close(done) }()
		io.Copy(c, up)
		c.Close()
		up.Close()
		<-done
		return
	}
	if port != 443 {
		return
	}
	hello, host, err := ReadClientHello(c)
	if err != nil {
		return
	}
	e.mu.Lock()
	blocked := e.status.State == "blocked"
	e.mu.Unlock()
	if blocked {
		return
	}
	// The original destination is authoritative, including ECH outer names.
	// All TLS from YouTube uses desync, including account and image endpoints.
	e.forwardTLS(c, host, []net.IP{destination}, hello, true, MatchHost(host, DefaultHosts))
}

func (e *Engine) StrategiesJSON() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	type choice struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	choices := []choice{{"auto", "Автоподбор"}}
	for _, s := range e.config.Strategies {
		choices = append(choices, choice{s.ID, s.Name})
	}
	b, _ := json.Marshal(choices)
	return string(b)
}

// DNS answers are obtained through certificate-validated DoH, never the ISP DNS.
// HTTPS/SVCB records are deliberately empty: HTTP/3 isn't supported by this
// TCP desync engine, and the client must negotiate HTTPS over TCP instead.
func (e *Engine) DNS(query []byte) []byte {
	return answerDNS(e.ctx, query, e.resolver.Resolve)
}

func answerDNS(ctx context.Context, query []byte, resolve func(context.Context, string) ([]net.IP, error)) []byte {
	var req dnsmessage.Message
	if len(query) > 4096 || req.Unpack(query) != nil || req.Header.Response || req.Header.OpCode != 0 || len(req.Questions) != 1 {
		return nil
	}
	q := req.Questions[0]
	res := dnsmessage.Message{Header: dnsmessage.Header{ID: req.ID, Response: true, RecursionDesired: req.RecursionDesired, RecursionAvailable: true}, Questions: req.Questions}
	if q.Class != dnsmessage.ClassINET {
		res.RCode = dnsmessage.RCodeRefused
	} else if q.Type == dnsmessage.TypeA || q.Type == dnsmessage.TypeAAAA {
		ips, err := resolve(ctx, normalizeHost(q.Name.String()))
		if err != nil {
			res.RCode = dnsmessage.RCodeServerFailure
		} else {
			for _, ip := range ips {
				if !publicIP(ip) {
					continue
				}
				h := dnsmessage.ResourceHeader{Name: q.Name, Type: q.Type, Class: dnsmessage.ClassINET, TTL: 60}
				if v4 := ip.To4(); q.Type == dnsmessage.TypeA && v4 != nil {
					var addr [4]byte
					copy(addr[:], v4)
					res.Answers = append(res.Answers, dnsmessage.Resource{Header: h, Body: &dnsmessage.AResource{A: addr}})
				} else if q.Type == dnsmessage.TypeAAAA && ip.To4() == nil && ip.To16() != nil {
					var addr [16]byte
					copy(addr[:], ip.To16())
					res.Answers = append(res.Answers, dnsmessage.Resource{Header: h, Body: &dnsmessage.AAAAResource{AAAA: addr}})
				}
			}
		}
	}
	b, _ := res.Pack()
	return b
}

func (e *Engine) serveDNSTCP(c net.Conn) {
	for {
		c.SetDeadline(time.Now().Add(10 * time.Second))
		var header [2]byte
		if _, err := io.ReadFull(c, header[:]); err != nil {
			return
		}
		n := int(binary.BigEndian.Uint16(header[:]))
		if n > 4096 || n < 12 {
			return
		}
		b := make([]byte, n)
		if _, err := io.ReadFull(c, b); err != nil {
			return
		}
		out := e.DNS(b)
		if out == nil {
			return
		}
		binary.BigEndian.PutUint16(header[:], uint16(len(out)))
		if _, err := c.Write(append(header[:], out...)); err != nil {
			return
		}
	}
}
