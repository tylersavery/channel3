// Package web carries the built web interface into the binary.
//
// The Vite build in this directory writes to dist/ui, which is embedded here
// and served by internal/api. dist/.gitkeep is committed so the embed pattern
// matches on a fresh clone where nothing has been built and Node may not even
// be installed: go build must never need npm.
package web

import "embed"

// Dist is the built web interface. The production UI is fs.Sub(Dist, "dist/ui").
//
// The all: prefix embeds files whose names begin with a dot, which is what
// keeps .gitkeep matching when dist/ui is absent.
//
//go:embed all:dist
var Dist embed.FS
