package app

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"unicode"

	"supportcenter/internal/database"
	"supportcenter/web"
)

// clientKey groups requests for rate limits. An IPv6 host usually controls a
// whole /64, so its addresses count as one client.
func clientKey(r *http.Request) string {
	ip, err := netip.ParseAddr(ClientRequestInfo(r).IP)
	if err != nil {
		return ""
	}
	if ip.Is6() {
		return netip.PrefixFrom(ip, 64).Masked().String()
	}
	return ip.String()
}

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
	info.Brand = web.BrandKey(r.Context())
	return info
}
