//go:build !notray

package tray

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"sync"
)

const iconSize = 32

var (
	iconMu    sync.Mutex
	iconCache = map[string][]byte{}
)

// iconPNG 生成 32x32 的状态图标 PNG.
//
// 使用简单圆点而不是外部资源, 避免引入二进制文件.
func iconPNG(kind string) []byte {
	iconMu.Lock()
	defer iconMu.Unlock()
	if cached, ok := iconCache[kind]; ok {
		return cached
	}
	img := image.NewNRGBA(image.Rect(0, 0, iconSize, iconSize))
	base := iconColor(kind)
	center := float64(iconSize) / 2
	radius := 13.0
	for y := 0; y < iconSize; y++ {
		for x := 0; x < iconSize; x++ {
			dx := float64(x) + 0.5 - center
			dy := float64(y) + 0.5 - center
			d := dx*dx + dy*dy
			if d > radius*radius {
				continue
			}
			// 边缘做一格抗锯齿.
			alpha := uint8(255)
			if d > (radius-1)*(radius-1) {
				alpha = 160
			}
			img.SetNRGBA(x, y, color.NRGBA{R: base.R, G: base.G, B: base.B, A: alpha})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	data := buf.Bytes()
	iconCache[kind] = data
	return data
}

func iconColor(kind string) color.NRGBA {
	switch kind {
	case "ok":
		return color.NRGBA{R: 47, G: 158, B: 68, A: 255}
	case "warning":
		return color.NRGBA{R: 230, G: 165, B: 0, A: 255}
	case "error":
		return color.NRGBA{R: 201, G: 42, B: 42, A: 255}
	default:
		return color.NRGBA{R: 134, G: 142, B: 150, A: 255}
	}
}
