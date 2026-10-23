package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

type replayConn struct {
	net.Conn
	prefix io.Reader
}

func (c *replayConn) Read(b []byte) (int, error) { return c.prefix.Read(b) }

// A successful TCP connect does not prove that a CDN route passes TLS. Race
// the original ClientHello, without decrypting or duplicating application data,
// and keep the first ServerHello. The app still validates its TLS certificate.
func firstTLSFlight(ctx context.Context, initial net.Conn, hello []byte,
	alternates func(context.Context) []net.IP,
	dial func(context.Context, net.IP) (net.Conn, error)) (net.Conn, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		conn net.Conn
		err  error
	}
	results := make(chan result)
	var wg sync.WaitGroup
	attempt := func(c net.Conn, ip net.IP) {
		defer wg.Done()
		var err error
		if c == nil {
			c, err = dial(ctx, ip)
		}
		if err == nil {
			raw := c
			stop := context.AfterFunc(ctx, func() { raw.Close() })
			if deadline, ok := ctx.Deadline(); ok {
				c.SetDeadline(deadline)
			}
			_, err = c.Write(hello)
			var record []byte
			if err == nil {
				header := make([]byte, 5)
				_, err = io.ReadFull(c, header)
				if err == nil {
					n := int(binary.BigEndian.Uint16(header[3:]))
					if header[0] != 22 || header[1] != 3 || n < 4 || n > 18432 {
						err = errors.New("upstream did not return a TLS ServerHello")
					} else {
						record = append(header, make([]byte, n)...)
						_, err = io.ReadFull(c, record[5:])
						if err == nil && record[5] != 2 {
							err = errors.New("invalid TLS ServerHello")
						}
					}
				}
			}
			stop()
			if err == nil {
				c.SetDeadline(time.Time{})
				c = &replayConn{c, io.MultiReader(bytes.NewReader(record), c)}
			}
		}
		if err != nil && c != nil {
			c.Close()
			c = nil
		}
		select {
		case results <- result{c, err}:
		case <-ctx.Done():
			if c != nil {
				c.Close()
			}
		}
	}
	wg.Add(2)
	go attempt(initial, nil)
	go func() {
		defer wg.Done()
		timer := time.NewTimer(250 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return
		}
		ips := alternates(ctx)
		seen := map[string]bool{}
		for _, ip := range ips {
			if ctx.Err() != nil {
				return
			}
			if seen[ip.String()] {
				continue
			}
			seen[ip.String()] = true
			if len(seen) > 6 {
				break
			}
			wg.Add(1)
			go attempt(nil, ip)
		}
	}()
	go func() { wg.Wait(); close(results) }()
	last := errors.New("no TLS response")
	for {
		select {
		case r, ok := <-results:
			if !ok {
				return nil, last
			}
			if r.err == nil && r.conn != nil {
				return r.conn, nil
			}
			if r.err != nil {
				last = r.err
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
