// Package webui 内嵌 Web GUI 静态资源.
//
// 构建流程把 web/dist 的内容复制到内部 dist 目录, 再通过 go:embed 打包.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed dist
var embedded embed.FS

// FS 是内嵌静态资源的根, 包含 index.html 与 assets.
var FS fs.FS

func init() {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic("初始化 Web 静态资源失败: " + err.Error())
	}
	FS = sub
}
