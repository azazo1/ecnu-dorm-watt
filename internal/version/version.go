package version

import "runtime/debug"

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// Info 返回版本, 提交与构建时间.
func Info() (string, string, string) {
	if Version != "dev" {
		return Version, Commit, BuildTime
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && Commit == "unknown" {
				Commit = s.Value
			}
		}
	}
	return Version, Commit, BuildTime
}
