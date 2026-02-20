package main

import (
	"github.com/caioricciuti/ch-ui/cmd"
	"github.com/caioricciuti/ch-ui/internal/version"
)

var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

func main() {
	version.Set(Version, Commit, BuildDate)
	cmd.FrontendFS = frontendFS()
	cmd.Execute()
}
