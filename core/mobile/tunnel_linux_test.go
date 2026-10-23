//go:build linux && !android

package mobile

import (
	"encoding/binary"
	"testing"
	"time"

	core "github.com/sonjiwu2/youtube-plugin-android/core"
	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/sys/unix"
)

// A datagram socket pair stands in for Android's TUN. This checks real IP/UDP
// decoding and DNS delivery through netstack, not just a mocked handler.
func TestTunnelDNSAndDescriptorOwnership(t *testing.T) {
	fd, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd[0])
	defer unix.Close(fd[1])
	e, err := core.Start(core.Options{Direct: true})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	tunnel, err := startTunnel(fd[0], e)
	if err != nil {
		t.Fatal(err)
	}
	defer tunnel.stop()
	name, _ := dnsmessage.NewName("youtube.com.")
	q := dnsmessage.Message{Header: dnsmessage.Header{ID: 327, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.Type(65), Class: dnsmessage.ClassINET}}}
	dns, _ := q.Pack()
	packet := make([]byte, 28+len(dns))
	packet[0] = 0x45
	packet[8] = 64
	packet[9] = 17
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	copy(packet[12:20], []byte{10, 77, 0, 2, 10, 77, 0, 1})
	binary.BigEndian.PutUint16(packet[20:22], 31000)
	binary.BigEndian.PutUint16(packet[22:24], 53)
	binary.BigEndian.PutUint16(packet[24:26], uint16(8+len(dns)))
	copy(packet[28:], dns)
	var sum uint32
	for i := 0; i < 20; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(packet[i:]))
	}
	for sum > 65535 {
		sum = sum&65535 + sum>>16
	}
	binary.BigEndian.PutUint16(packet[10:12], ^uint16(sum))
	if _, err := unix.Write(fd[1], packet); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	deadline := time.Now().Add(3 * time.Second)
	for {
		n, err := unix.Read(fd[1], buf)
		if err == unix.EAGAIN && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if n < 40 {
			t.Fatalf("short packet: %x", buf[:n])
		}
		var answer dnsmessage.Message
		if err := answer.Unpack(buf[28:n]); err != nil {
			t.Fatal(err)
		}
		if !answer.Response || answer.ID != 327 || answer.RCode != dnsmessage.RCodeSuccess {
			t.Fatalf("%+v", answer)
		}
		break
	}
	tunnel.stop()
	if _, err := unix.FcntlInt(uintptr(fd[0]), unix.F_GETFD, 0); err != nil {
		t.Fatalf("closed caller FD: %v", err)
	}
}
