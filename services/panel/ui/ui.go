package ui

import (
	"embed"
	"io/fs"
)

//go:embed *
var files embed.FS

func Dist() fs.FS {
	sub, err := fs.Sub(files, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
