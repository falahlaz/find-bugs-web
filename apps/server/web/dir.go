//go:build !embed_web

// Package web holds the React build. Without the embed_web build tag the
// build is read from a directory at runtime (WEB_DIR), which suits development.
package web

import (
	"io/fs"
	"os"
)

// FS returns the build directory, or nil if it does not exist.
func FS(dir string) (fs.FS, error) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, nil
	}
	return os.DirFS(dir), nil
}
