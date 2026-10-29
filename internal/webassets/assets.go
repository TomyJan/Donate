package webassets

import (
	"embed"
	"io/fs"
)

// Build the production frontend before compiling the release binary.
//
//go:embed all:dist
var embedded embed.FS

func FS() fs.FS {
	f, e := fs.Sub(embedded, "dist")
	if e != nil {
		panic(e)
	}
	return f
}
