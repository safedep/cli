package version

var (
	Version = "v0.0.0"
	Commit  = "unknown"
)

func String() string {
	return Version + " (" + Commit + ")"
}
