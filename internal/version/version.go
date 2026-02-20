package version

var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

func Set(v, c, d string) {
	Version = v
	Commit = c
	BuildDate = d
}
