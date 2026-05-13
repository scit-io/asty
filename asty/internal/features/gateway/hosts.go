package gateway

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/rs/zerolog"
)

// allowedHostSet is a set of Origin hosts (host or host:port) that the
// gateway permits. An empty set disables Origin checking (dev mode).
type allowedHostSet map[string]struct{}

// parseAllowedHosts builds an allowedHostSet from config entries. Each
// entry may be a bare host ("example.com"), host:port ("localhost:3000"),
// or a full URL ("http://localhost:3000") — in the latter case only the
// host part is kept.
func parseAllowedHosts(log zerolog.Logger, entries []string) (allowedHostSet, error) {
	set := make(allowedHostSet)
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		host, err := extractHost(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid host %q: %w", entry, err)
		}
		set[host] = struct{}{}
	}

	if len(set) > 0 {
		hosts := make([]string, 0, len(set))
		for h := range set {
			hosts = append(hosts, h)
		}
		log.Info().Strs("hosts", hosts).Msg("allowed Origin hosts")
	}
	return set, nil
}

// allows reports whether the given Origin header is permitted.
//
// Rules (in priority order):
//  1. Empty set (no allowed hosts configured) — allow everything.
//  2. Empty origin (curl, server-to-server, health probes) — allow.
//  3. Otherwise extract host from Origin and look it up.
func (s allowedHostSet) allows(log zerolog.Logger, origin string) bool {
	if len(s) == 0 {
		return true
	}
	if origin == "" {
		return true
	}
	host, err := extractHost(origin)
	if err != nil {
		log.Warn().Str("origin", origin).Err(err).Msg("invalid Origin")
		return false
	}
	_, ok := s[host]
	return ok
}

func extractHost(s string) (string, error) {
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", err
		}
		if u.Host == "" {
			return "", fmt.Errorf("cannot extract host from %q", s)
		}
		return u.Host, nil
	}
	return s, nil
}
