// Package web embeds the static frontend assets into the binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var embedded embed.FS

// FS returns the static asset filesystem rooted at the static/ directory.
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "static")
	if err != nil {
		panic(err) // embed guarantees the directory exists at build time
	}
	return sub
}
