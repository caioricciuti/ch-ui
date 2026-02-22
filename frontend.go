package main

import (
	"embed"
	"io/fs"
	"log/slog"
)

//go:embed all:ui/dist
var uiDistFS embed.FS

func frontendFS() fs.FS {
	sub, err := fs.Sub(uiDistFS, "ui/dist")
	if err != nil {
		slog.Warn("Failed to open embedded frontend directory", "error", err)
		return nil
	}
	return sub
}
