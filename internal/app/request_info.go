package app

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"unicode"

	"supportcenter/internal/database"
)

func ClientRequestInfo(r *http.Request) database.RequestInfo {
	var info database.RequestInfo
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		if peer, err := netip.ParseAddr(host); err == nil {
			info.IP = peer.Unmap().String()
			// The public site is forwarded through the host's loopback-bound Tunnel.
			// Docker may present that local peer as its private bridge gateway.
			if peer.IsLoopback() || peer.IsPrivate() {
				if forwarded, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("CF-Connecting-IP"))); err == nil {
					info.IP = forwarded.Unmap().String()
				}
			}
		}
	}
	agent := strings.ToValidUTF8(r.UserAgent(), "")
	agent = strings.Map(func(ch rune) rune {
		if unicode.IsControl(ch) {
			return -1
		}
		return ch
	}, agent)
	if chars := []rune(agent); len(chars) > 512 {
		agent = string(chars[:512])
	}
	info.UserAgent = agent
	return info
}
