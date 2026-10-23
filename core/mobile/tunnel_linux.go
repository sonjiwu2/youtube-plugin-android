//go:build linux || android

package mobile

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	core "github.com/sonjiwu2/youtube-plugin-android/core"
	netstack "github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/adapter"
	"github.com/xjasonlyu/tun2socks/v2/core/device"
	"github.com/xjasonlyu/tun2socks/v2/core/device/fdbased"
	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

type vpnTunnel struct {
	device device.Device
	stack  *stack.Stack
	cancel context.CancelFunc
	mu     sync.Mutex
	closed bool
	conns  map[net.Conn]bool
	wg     sync.WaitGroup
	sem    chan struct{}
	engine *core.Engine
}

// startTunnel duplicates the descriptor; the caller keeps ownership of its FD.
func startTunnel(fd int, engine *core.Engine) (*vpnTunnel, error) {
	if fd < 0 {
		return nil, errors.New("invalid TUN descriptor")
	}
	dup, err := unix.Dup(fd)
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(dup)
	dev, err := fdbased.Open(strconv.Itoa(dup), 1500, 0)
	if err != nil {
		unix.Close(dup)
		return nil, err
	}
	_, cancel := context.WithCancel(context.Background())
	t := &vpnTunnel{device: dev, engine: engine, cancel: cancel, conns: map[net.Conn]bool{}, sem: make(chan struct{}, 64)}
	s, err := netstack.CreateStack(&netstack.Config{LinkEndpoint: dev, TransportHandler: t})
	if err != nil {
		dev.Close()
		cancel()
		return nil, err
	}
	t.stack = s
	return t, nil
}

func (t *vpnTunnel) handle(c net.Conn, run func()) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		c.Close()
		return
	}
	select {
	case t.sem <- struct{}{}:
	default:
		t.mu.Unlock()
		c.Close()
		return
	}
	t.wg.Add(1)
	t.conns[c] = true
	t.mu.Unlock()
	go func() {
		defer t.wg.Done()
		defer func() { <-t.sem; c.Close(); t.mu.Lock(); delete(t.conns, c); t.mu.Unlock() }()
		run()
	}()
}

func (t *vpnTunnel) HandleTCP(c adapter.TCPConn) {
	id := c.ID()
	ip := net.IP(id.LocalAddress.AsSlice())
	t.handle(c, func() { t.engine.ServeTCP(c, ip, id.LocalPort) })
}

func (t *vpnTunnel) HandleUDP(c adapter.UDPConn) {
	// QUIC cannot pass through the TCP desync engine; closing its endpoint makes
	// YouTube fall back to TCP. There is no direct UDP fallback around the VPN.
	if c.ID().LocalPort != 53 {
		c.Close()
		return
	}
	t.handle(c, func() {
		buf := make([]byte, 4096)
		for {
			c.SetReadDeadline(time.Now().Add(8 * time.Second))
			n, addr, err := c.ReadFrom(buf)
			if err != nil {
				return
			}
			out := t.engine.DNS(buf[:n])
			if out == nil {
				continue
			}
			c.SetWriteDeadline(time.Now().Add(3 * time.Second))
			if _, err = c.WriteTo(out, addr); err != nil {
				return
			}
		}
	})
}

func (t *vpnTunnel) stop() {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	t.cancel()
	for c := range t.conns {
		c.Close()
	}
	t.mu.Unlock()
	t.device.Close()
	t.stack.Close()
	t.wg.Wait()
	t.stack.Wait()
}
