package agent

import (
	"net"
	"os"
	"strings"

	"github.com/rs/zerolog/log"
)

// resolveNATSPeers returns the IPs of OTHER cluster nodes for the
// rendered cluster.routes. Sources in priority order:
//  1. cfg.NATS.PeersFile (env A_NATS_PEERS_FILE) — one IP per line;
//     dev's DNS-A-record stand-in.
//  2. cfg.NATS.Peers (env A_NATS_PEERS) — comma-separated; static, e.g. CI.
//  3. DNS LookupIP(cfg.Domain) — prod path.
//
// Self-IP is filtered so a node never routes to itself. Env values
// arrive through core/config; this function does not call os.Getenv
// directly.
func (a *Agent) resolveNATSPeers(selfIP string) []string {
	if path := a.cfg.NATS.PeersFile; path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			return filterSelf(splitAndTrim(string(raw)), selfIP)
		}
	}
	if raw := a.cfg.NATS.Peers; raw != "" {
		return filterSelf(splitAndTrim(raw), selfIP)
	}
	if a.cfg.Domain == "" {
		return nil
	}
	ips, err := net.LookupIP(a.cfg.Domain)
	if err != nil {
		log.Warn().Err(err).Str("domain", a.cfg.Domain).Msg("NATS peer discovery: DNS lookup failed")
		return nil
	}
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		if ip.To4() == nil {
			continue
		}
		out = append(out, ip.String())
	}
	return filterSelf(out, selfIP)
}

// splitAndTrim splits on commas AND any whitespace (incl. newlines)
// so the same parser handles both env-var ("a,b,c") and peers-file
// ("a\nb\nc") forms.
func splitAndTrim(raw string) []string {
	sep := func(r rune) bool { return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' ' }
	out := make([]string, 0)
	for _, p := range strings.FieldsFunc(raw, sep) {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// filterSelf removes selfIP from ips so a node never routes to itself.
func filterSelf(ips []string, self string) []string {
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		if ip != self {
			out = append(out, ip)
		}
	}
	return out
}
