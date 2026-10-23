//go:build !linux && !android

package mobile

import (
	"errors"
	core "github.com/sonjiwu2/youtube-plugin-android/core"
)

type vpnTunnel struct{}

func startTunnel(fd int, engine *core.Engine) (*vpnTunnel, error) {
	return nil, errors.New("TUN requires Android/Linux")
}
func (t *vpnTunnel) stop() {}
