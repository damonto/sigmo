package web

import (
	"embed"
	"io/fs"
)

//go:embed dist
var dist embed.FS

func Root() fs.FS {
	root, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return root
}
