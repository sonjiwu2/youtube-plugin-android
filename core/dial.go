package core

import (
	"context"
	"errors"
	"net"
	"time"
)

// Числовые адреса пробуются с перекрытием: один «чёрный» маршрут не задерживает остальные.
func dialCandidates(ctx context.Context, ips []net.IP, dial func(context.Context, net.IP) (net.Conn, error)) (net.Conn, error) {
	var ipv4, ipv6, ordered []net.IP
	for _, ip := range ips {
		if ip.To4() != nil {
			ipv4 = append(ipv4, ip)
		} else {
			ipv6 = append(ipv6, ip)
		}
	}
	for i := 0; i < len(ipv4) || i < len(ipv6); i++ {
		if i < len(ipv4) {
			ordered = append(ordered, ipv4[i])
		}
		if i < len(ipv6) {
			ordered = append(ordered, ipv6[i])
		}
	}
	ips = ordered
	if len(ips) > 8 {
		ips = ips[:8]
	}
	if len(ips) == 0 {
		return nil, errors.New("no public addresses")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		conn net.Conn
		err  error
	}
	results := make(chan result)
	for i, ip := range ips {
		go func(i int, ip net.IP) {
			if i > 0 {
				timer := time.NewTimer(time.Duration(i) * 200 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
				}
			}
			c, err := dial(ctx, ip)
			select {
			case results <- result{c, err}:
			case <-ctx.Done():
				if c != nil {
					c.Close()
				}
			}
		}(i, ip)
	}
	var last error
	for range ips {
		select {
		case r := <-results:
			if r.err == nil && r.conn != nil {
				return r.conn, nil
			}
			last = r.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if last == nil {
		last = errors.New("all addresses unavailable")
	}
	return nil, last
}
