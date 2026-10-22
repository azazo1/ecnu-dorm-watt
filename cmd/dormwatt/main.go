package main

import (
	"os"
	// 内嵌 IANA 时区数据库, 保证在缺少系统时区数据的平台 (如未安装 tzdata 的 Windows) 上仍能解析 Asia/Shanghai.
	_ "time/tzdata"

	"github.com/azazo1/ecnu-dorm-watt/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:]))
}
