// Package assets exposes the binaries and scripts bundled into the hexplus executable.
//
// Real bundled binaries live under bin/ and are produced by build/build-statics.sh
// before the final `go build`. During Phase 0 we ship placeholder files so the
// extract path can be wired up and tested without waiting on the static builds.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed bin/*
var binFS embed.FS

// Binaries returns the embedded binaries subtree rooted at "bin/".
// Each entry's name is the basename (e.g. "openvpn"), without the "bin/" prefix.
func Binaries() fs.FS {
	sub, err := fs.Sub(binFS, "bin")
	if err != nil {
		// Sub only errors on invalid dir name, which is a programming error.
		panic(err)
	}
	return sub
}
