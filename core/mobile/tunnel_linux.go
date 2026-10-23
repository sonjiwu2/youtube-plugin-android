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
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

type vpnTunnel struct {
	device device.Device
	stack  *stack.Stack
	cancel context.CancelFunc
	ctx    context.Context
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
	ctx, cancel := context.WithCancel(context.Background())
	t := &vpnTunnel{device: dev, engine: engine, ctx: ctx, cancel: cancel, conns: map[net.Conn]bool{}, sem: make(chan struct{}, 128)}
	link := &deferredLink{LinkEndpoint: dev}
	s, err := netstack.CreateStack(&netstack.Config{LinkEndpoint: link, TransportHandler: t})
	if err != nil {
		dev.Close()
		cancel()
		return nil, err
	}
	t.stack = s
	tcpForwarder := tcp.NewForwarder(s, 0, 256, func(r *tcp.ForwarderRequest) {
		t.mu.Lock()
		if t.closed {
			t.mu.Unlock()
			r.Complete(true)
			return
		}
		select {
		case t.sem <- struct{}{}:
		default:
			t.mu.Unlock()
			r.Complete(true)
			return
		}
		t.wg.Add(1)
		t.mu.Unlock()
		go func() {
			defer t.wg.Done()
			defer func() { <-t.sem }()
			id := r.ID()
			ip := net.IP(id.LocalAddress.AsSlice())
			upstream, strategy, err := engine.PrepareTCP(t.ctx, ip, id.LocalPort)
			if err != nil {
				r.Complete(true)
				return
			}
			if upstream != nil {
				defer upstream.Close()
			}
			var queue waiter.Queue
			endpoint, failure := r.CreateEndpoint(&queue)
			if failure != nil {
				r.Complete(true)
				return
			}
			r.Complete(false)
			endpoint.SocketOptions().SetKeepAlive(true)
			idle := tcpip.KeepaliveIdleOption(60 * time.Second)
			endpoint.SetSockOpt(&idle)
			conn := &preparedTCP{TCPConn: gonet.NewTCPConn(&queue, endpoint), upstream: upstream, strategy: strategy}
			defer conn.Close()
			t.mu.Lock()
			if t.closed {
				t.mu.Unlock()
				return
			}
			t.conns[conn] = true
			t.mu.Unlock()
			defer func() { t.mu.Lock(); delete(t.conns, conn); t.mu.Unlock() }()
			engine.ServeTCP(conn, ip, id.LocalPort)
		}()
	})
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpForwarder.HandlePacket)
	// Return unhandled for QUIC so netstack emits ICMP port-unreachable. Merely
	// dropping UDP makes clients wait through QUIC timeouts before trying TCP.
	dnsForwarder := udp.NewForwarder(s, func(r *udp.ForwarderRequest) bool {
		var queue waiter.Queue
		endpoint, err := r.CreateEndpoint(&queue)
		if err != nil {
			return false
		}
		t.HandleUDP(&dnsConnection{UDPConn: gonet.NewUDPConn(&queue, endpoint), id: r.ID()})
		return true
	})
	s.SetTransportProtocolHandler(udp.ProtocolNumber, func(id stack.TransportEndpointID, packet *stack.PacketBuffer) bool {
		if id.LocalPort != 53 {
			return false
		}
		return dnsForwarder.HandlePacket(id, packet)
	})
	link.start()
	return t, nil
}

type preparedTCP struct {
	*gonet.TCPConn
	upstream net.Conn
	strategy string
}

func (c *preparedTCP) VPNUpstream() (net.Conn, string) { return c.upstream, c.strategy }
func (c *preparedTCP) Close() error {
	if c.upstream != nil {
		c.upstream.Close()
	}
	return c.TCPConn.Close()
}

// Protocol handlers must be completely initialized before the descriptor's
// reader starts. NIC construction normally attaches and starts it immediately.
type deferredLink struct {
	stack.LinkEndpoint
	mu         sync.Mutex
	dispatcher stack.NetworkDispatcher
	started    bool
}

func (l *deferredLink) Attach(d stack.NetworkDispatcher) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.dispatcher = d
	if l.started {
		l.LinkEndpoint.Attach(d)
	}
}
func (l *deferredLink) IsAttached() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dispatcher != nil
}
func (l *deferredLink) start() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.started = true
	l.LinkEndpoint.Attach(l.dispatcher)
}

type dnsConnection struct {
	*gonet.UDPConn
	id stack.TransportEndpointID
}

func (c *dnsConnection) ID() stack.TransportEndpointID { return c.id }

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
