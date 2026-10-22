//go:build !notray

package tray

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"sync"
)

const iconSize = 32

//go:embed assets/icon.png
var iconBasePNG []byte

var (
	iconMu    sync.Mutex
	iconCache = map[string][]byte{}
	iconBase  image.Image
	baseOnce  sync.Once
)

// iconPNG 生成 32x32 的状态图标 PNG.
//
// 基础图标来自通知使用的 DormWatt 图标, 右上角叠加健康状态圆点.
func iconPNG(kind string) []byte {
	iconMu.Lock()
	defer iconMu.Unlock()
	if cached, ok := iconCache[kind]; ok {
		return cached
	}
	img := image.NewNRGBA(image.Rect(0, 0, iconSize, iconSize))
	if base := baseIcon(); base != nil {
		draw.Draw(img, img.Bounds(), base, image.Point{}, draw.Src)
	}
	drawStatusDot(img, iconColor(kind))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	data := buf.Bytes()
	iconCache[kind] = data
	return data
}

// baseIcon 解码内嵌的应用图标, 只执行一次.
func baseIcon() image.Image {
	baseOnce.Do(func() {
		img, err := png.Decode(bytes.NewReader(iconBasePNG))
		if err == nil {
			iconBase = img
		}
	})
	return iconBase
}

// drawStatusDot 在图标右上角绘制带白色描边的状态圆点.
func drawStatusDot(img *image.NRGBA, base color.NRGBA) {
	const (
		cx     = 27.0
		cy     = 5.0
		outerR = 3.5
		innerR = 2.3
		white  = 240
	)
	for y := 0; y < 10; y++ {
		for x := 22; x < 32; x++ {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			d := dx*dx + dy*dy
			switch {
			case d <= innerR*innerR:
				img.SetNRGBA(x, y, base)
			case d <= outerR*outerR:
				img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: white})
			}
		}
	}
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
