//go:build embed_web

// Package web holds the React build. With the embed_web build tag (used by
// `make build`) the files in web/dist are compiled into the binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the embedded build.
func FS(string) (fs.FS, error) { return fs.Sub(dist, "dist") }
