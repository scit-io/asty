package server

import (
	"os"
	"runtime"
)

// resolveArtifactURL substitutes well-known placeholders in an artifact
// URL template. Recognised keys:
//
//   - ${VERSION}     — from the caller (deployer plan or alloc.Version)
//   - ${ARCH}        — cfg.Artifact.Arch, fallback to runtime.GOARCH
//   - ${GITHUB_REPO} — cfg.Artifact.GitHubRepo
//
// Unknown placeholders are left as-is so a misconfigured URL fails loudly
// at the downloader (a literal "${FOO}" produces a 404, easy to debug)
// rather than silently expanding to empty.
//
// Expansion happens server-side so agents receive a fully resolved URL —
// they don't need to know about GITHUB_REPO or what ARCH to target.
// Config values are read from s.cfg.Artifact (env A_ARCH / A_GITHUB_REPO
// flow through core/config in line with the single-config-path
// discipline).
func (s *Server) resolveArtifactURL(urlTemplate, version string) string {
	arch := s.cfg.Artifact.Arch
	if arch == "" {
		arch = runtime.GOARCH
	}
	repo := s.cfg.Artifact.GitHubRepo

	return os.Expand(urlTemplate, func(key string) string {
		switch key {
		case "VERSION":
			return version
		case "ARCH":
			return arch
		case "GITHUB_REPO":
			return repo
		}
		return "${" + key + "}" // preserve unknown placeholders
	})
}
