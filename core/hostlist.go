package core

import (
	"net"
	"strings"
)

var DefaultHosts = []string{"youtube.com", "youtu.be", "googlevideo.com", "ytimg.com", "ggpht.com", "youtubei.googleapis.com", "youtube.googleapis.com", "youtube-nocookie.com", "youtubeembeddedplayer.googleapis.com", "yt3.googleusercontent.com", "jnn-pa.googleapis.com", "wide-youtube.l.google.com", "youtube-ui.l.google.com", "yt-video-upload.l.google.com", "gstatic.com"}

func normalizeHost(host string) string { return strings.ToLower(strings.TrimSuffix(host, ".")) }

func ValidHost(host string) bool {
	if len(host) == 0 || len(host) > 253 || net.ParseIP(host) != nil {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func MatchHost(host string, rules []string) bool {
	host = normalizeHost(host)
	for _, rule := range rules {
		exact := strings.HasPrefix(rule, "^")
		rule = normalizeHost(strings.TrimPrefix(rule, "^"))
		if host == rule || !exact && strings.HasSuffix(host, "."+rule) {
			return true
		}
	}
	return false
}

func publicIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32"} {
		_, n, _ := net.ParseCIDR(cidr)
		if n.Contains(ip) {
			return false
		}
	}
	return true
}
