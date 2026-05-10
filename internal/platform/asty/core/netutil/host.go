package netutil

import (
	"net"
	"os"

	"github.com/rs/zerolog/log"
)

// Hostname returns the OS hostname or "unknown" if the lookup fails. Agents
// and servers use this as a stable node ID when one is not provided via
// configuration.
func Hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return name
}

// LocalIPv4 returns the address other nodes should use to reach this node.
//
// If natsHost is a loopback address (e.g. 127.0.0.2 in local multi-node
// dev mode), it is returned as-is — peers on the same machine connect to
// the same loopback.
//
// Otherwise the first non-loopback IPv4 interface address is returned, or
// "" if none is found.
func LocalIPv4(natsHost string) string {
	if ip := net.ParseIP(natsHost); ip != nil && ip.IsLoopback() {
		return natsHost
	}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get network interfaces")
		return ""
	}

	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}

	log.Warn().Msg("no non-loopback IP address found")
	return ""
}
