package api

import (
	"net"
	"net/http"
	"strings"
)

func parseCIDRList(value string) ([]*net.IPNet, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}

	parts := strings.Split(trimmed, ",")
	out := make([]*net.IPNet, 0, len(parts))
	for _, part := range parts {
		cidr := strings.TrimSpace(part)
		if cidr == "" {
			continue
		}
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, err
		}
		out = append(out, ipnet)
	}
	return out, nil
}

func parseRemoteIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err == nil {
		return net.ParseIP(strings.TrimSpace(host))
	}
	return net.ParseIP(strings.TrimSpace(remoteAddr))
}

func ipInCIDRs(ip net.IP, cidrs []*net.IPNet) bool {
	for _, cidr := range cidrs {
		if cidr != nil && cidr.Contains(ip) {
			return true
		}
	}
	return false
}

func internalRequestAllowed(r *http.Request, allowedCIDRs []*net.IPNet) bool {
	if len(allowedCIDRs) == 0 {
		return false
	}
	clientIP := parseRemoteIP(r.RemoteAddr)
	if clientIP == nil {
		return false
	}
	return ipInCIDRs(clientIP, allowedCIDRs)
}
