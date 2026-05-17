package server

import (
	"runtime"
	"testing"
)

func TestResolveArtifactURL(t *testing.T) {
	t.Setenv("A_GITHUB_REPO", "acme/asty")
	t.Setenv("A_ARCH", "")

	cases := []struct {
		name     string
		template string
		version  string
		want     string
	}{
		{
			name:     "no placeholders",
			template: "https://example.com/static.tar.gz",
			version:  "v1.2.3",
			want:     "https://example.com/static.tar.gz",
		},
		{
			name:     "local artifact",
			template: "local",
			version:  "v1.2.3",
			want:     "local",
		},
		{
			name:     "all three placeholders",
			template: "https://github.com/${GITHUB_REPO}/releases/download/${VERSION}/service_linux_${ARCH}.tar.gz",
			version:  "v1.2.3",
			want:     "https://github.com/acme/asty/releases/download/v1.2.3/service_linux_" + runtime.GOARCH + ".tar.gz",
		},
		{
			name:     "unknown placeholder preserved",
			template: "https://${UNKNOWN}/${VERSION}",
			version:  "v1.2.3",
			want:     "https://${UNKNOWN}/v1.2.3",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolveArtifactURL(c.template, c.version)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestResolveArtifactURL_ARCHOverride(t *testing.T) {
	t.Setenv("A_ARCH", "arm64")
	t.Setenv("A_GITHUB_REPO", "")

	got := resolveArtifactURL("path-${ARCH}-${VERSION}", "v9.9.9")
	want := "path-arm64-v9.9.9"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
