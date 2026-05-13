package server

import (
	"os"
	"runtime"
)

// resolveArtifactURL substitutes well-known placeholders in an artifact
// URL template. Recognised keys:
//
//   - ${VERSION}     — from the caller (deployer plan or alloc.Version)
//   - ${ARCH}        — env A_ARCH, fallback to runtime.GOARCH (server's arch)
//   - ${GITHUB_REPO} — env A_GITHUB_REPO, blank if unset
//
// Unknown placeholders are left as-is so a misconfigured URL fails loudly
// at the downloader (a literal "${FOO}" produces a 404, easy to debug)
// rather than silently expanding to empty.
//
// Expansion happens server-side so agents receive a fully resolved URL —
// they don't need to know about GITHUB_REPO or what ARCH to target.
func resolveArtifactURL(urlTemplate, version string) string {
	arch := os.Getenv("A_ARCH")
	if arch == "" {
		arch = runtime.GOARCH
	}
	repo := os.Getenv("A_GITHUB_REPO")

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
