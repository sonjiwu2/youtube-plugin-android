package core

import (
	"encoding/binary"
	"errors"
	"io"
)

var ErrClientHello = errors.New("invalid TLS ClientHello")

// ReadClientHello сохраняет исходные рекорды: прокси не расшифровывает TLS.
func ReadClientHello(r io.Reader) ([]byte, string, error) {
	var wire, hello []byte
	for len(wire) < 65536 {
		header := make([]byte, 5)
		if _, err := io.ReadFull(r, header); err != nil {
			return nil, "", err
		}
		n := int(binary.BigEndian.Uint16(header[3:]))
		if header[0] != 22 || header[1] != 3 || n == 0 || n > 18432 {
			return nil, "", ErrClientHello
		}
		data := make([]byte, n)
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, "", err
		}
		wire = append(wire, header...)
		wire = append(wire, data...)
		hello = append(hello, data...)
		if len(hello) >= 4 {
			length := int(hello[1])<<16 | int(hello[2])<<8 | int(hello[3])
			if hello[0] != 1 || length > 65532 {
				return nil, "", ErrClientHello
			}
			if len(hello) >= length+4 {
				host, err := ParseSNI(hello[:length+4])
				return wire, host, err
			}
		}
	}
	return nil, "", ErrClientHello
}

// ParseSNI разбирает длины всех полей, включая неизвестные расширения.
func ParseSNI(h []byte) (string, error) {
	if len(h) < 39 || h[0] != 1 || (int(h[1])<<16|int(h[2])<<8|int(h[3])) != len(h)-4 {
		return "", ErrClientHello
	}
	take := func(p *int, n int) ([]byte, bool) {
		if n < 0 || *p+n > len(h) {
			return nil, false
		}
		b := h[*p : *p+n]
		*p += n
		return b, true
	}
	p := 38
	sid, ok := take(&p, 1)
	if !ok {
		return "", ErrClientHello
	}
	if _, ok = take(&p, int(sid[0])); !ok {
		return "", ErrClientHello
	}
	c, ok := take(&p, 2)
	if !ok {
		return "", ErrClientHello
	}
	if _, ok = take(&p, int(binary.BigEndian.Uint16(c))); !ok {
		return "", ErrClientHello
	}
	c, ok = take(&p, 1)
	if !ok {
		return "", ErrClientHello
	}
	if _, ok = take(&p, int(c[0])); !ok {
		return "", ErrClientHello
	}
	c, ok = take(&p, 2)
	if !ok || int(binary.BigEndian.Uint16(c)) != len(h)-p {
		return "", ErrClientHello
	}
	for p < len(h) {
		ext, ok := take(&p, 4)
		if !ok {
			return "", ErrClientHello
		}
		data, ok := take(&p, int(binary.BigEndian.Uint16(ext[2:])))
		if !ok {
			return "", ErrClientHello
		}
		if binary.BigEndian.Uint16(ext) != 0 {
			continue
		}
		if len(data) < 5 || int(binary.BigEndian.Uint16(data)) != len(data)-2 {
			return "", ErrClientHello
		}
		for i := 2; i < len(data); {
			if i+3 > len(data) {
				return "", ErrClientHello
			}
			kind := data[i]
			n := int(binary.BigEndian.Uint16(data[i+1:]))
			i += 3
			if i+n > len(data) {
				return "", ErrClientHello
			}
			host := normalizeHost(string(data[i : i+n]))
			i += n
			if kind == 0 {
				if !ValidHost(host) {
					return "", ErrClientHello
				}
				return host, nil
			}
		}
	}
	return "", ErrClientHello
}
