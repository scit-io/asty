package asty

import "asty/internal/platform/asty/features/deployment/artifacts"

// ArtifactDownloader — backward-compatible alias
type ArtifactDownloader = artifacts.Downloader

var NewArtifactDownloader = artifacts.NewDownloader
