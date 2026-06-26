package version

// These are set via -ldflags at build time. See Makefile.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func Full() string {
	return "hexplus " + Version + " (commit " + Commit + ", built " + Date + ")"
}
