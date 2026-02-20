package main

import (
	"embed"
	"io/fs"
)

//go:embed all:ui/dist
var uiDistFS embed.FS

func frontendFS() fs.FS {
	sub, _ := fs.Sub(uiDistFS, "ui/dist")
	return sub
}
