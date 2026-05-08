package webdist

import "embed"

const Root = "dist"

// FS contains the production Web console when built through build.sh or the
// Dockerfile. The tracked placeholder keeps regular Go builds working before a
// frontend build has populated dist.
//
//go:embed all:dist
var FS embed.FS
