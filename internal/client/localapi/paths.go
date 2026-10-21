package localapi

import "github.com/azazo1/ecnu-dorm-watt/internal/appdirs"

// defaultRuntimePath 返回默认 runtime.json 路径.
func defaultRuntimePath() (string, error) {
	return appdirs.RuntimePath()
}
