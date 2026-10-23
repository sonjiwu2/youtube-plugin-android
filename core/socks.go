package core

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

func dialSOCKS(ctx context.Context, proxy string, ip net.IP, port uint16) (net.Conn, error) {
	c, err := (&net.Dialer{Timeout: 7 * time.Second}).DialContext(ctx, "tcp", proxy)
	if err != nil {
		return nil, err
	}
	stopCancel := context.AfterFunc(ctx, func() { c.Close() })
	defer stopCancel()
	ok := false
	defer func() {
		if !ok {
			c.Close()
		}
	}()
	c.SetDeadline(time.Now().Add(8 * time.Second))
	if _, err = c.Write([]byte{5, 1, 0}); err != nil {
		return nil, err
	}
	var reply [4]byte
	if _, err = io.ReadFull(c, reply[:2]); err != nil || reply[0] != 5 || reply[1] != 0 {
		return nil, errors.New("SOCKS authentication failed")
	}
	req := []byte{5, 1, 0}
	if v4 := ip.To4(); v4 != nil {
		req = append(req, 1)
		req = append(req, v4...)
	} else {
		req = append(req, 4)
		req = append(req, ip.To16()...)
	}
	req = binary.BigEndian.AppendUint16(req, port)
	if _, err = c.Write(req); err != nil {
		return nil, err
	}
	if _, err = io.ReadFull(c, reply[:]); err != nil || reply[0] != 5 || reply[1] != 0 {
		return nil, fmt.Errorf("SOCKS connect failed (reply %d)", reply[1])
	}
	n := 0
	switch reply[3] {
	case 1:
		n = 4
	case 4:
		n = 16
	default:
		return nil, errors.New("SOCKS non-IP reply")
	}
	if _, err = io.CopyN(io.Discard, c, int64(n+2)); err != nil {
		return nil, err
	}
	c.SetDeadline(time.Time{})
	ok = true
	return c, nil
}
