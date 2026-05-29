package netutil

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/rs/zerolog"
)

// OriginAllowList is a set of permitted Origin hosts (host or
// host:port). An empty set disables Origin checking — the dev-mode
// "allow everything" default. Shared by the gateway (user traffic)
// and the dashboard (admin surface); both validate the browser's
// Origin header the same way.
type OriginAllowList map[string]struct{}

// ParseOriginAllowList builds an OriginAllowList from config entries.
// Each entry may be a bare host ("example.com"), host:port
// ("localhost:3000"), or a full URL ("http://localhost:3000") — only
// the host part is kept in the latter case.
func ParseOriginAllowList(log zerolog.Logger, entries []string) (OriginAllowList, error) {
	set := make(OriginAllowList)
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		host, err := ExtractHost(entry)
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

// Allows reports whether the given Origin header is permitted.
//
// Rules (in priority order):
//  1. Empty set (no allowed hosts configured) — allow everything.
//  2. Empty origin (curl, server-to-server, health probes) — allow.
//  3. Otherwise extract the host from Origin and look it up.
func (s OriginAllowList) Allows(log zerolog.Logger, origin string) bool {
	if len(s) == 0 {
		return true
	}
	if origin == "" {
		return true
	}
	host, err := ExtractHost(origin)
	if err != nil {
		log.Warn().Str("origin", origin).Err(err).Msg("invalid Origin")
		return false
	}
	_, ok := s[host]
	return ok
}

// ExtractHost returns the host[:port] portion of a value that may be a
// bare host, host:port, or a full URL.
func ExtractHost(s string) (string, error) {
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
