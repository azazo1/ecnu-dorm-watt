package notify

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync"

	"github.com/azazo1/ecnu-dorm-watt/internal/appdirs"
)

const iconSize = 64

var (
	iconOnce sync.Once
	iconPath string
	iconErr  error
)

// EnsureIcon 生成并返回通知图标路径.
func EnsureIcon() (string, error) {
	iconOnce.Do(func() {
		dir, err := appdirs.ClientRunDir()
		if err != nil {
			iconErr = err
			return
		}
		path := filepath.Join(dir, "notify-icon.png")
		if _, err := os.Stat(path); err == nil {
			iconPath = path
			return
		}
		data := renderIcon()
		if err := os.WriteFile(path, data, 0o600); err != nil {
			iconErr = err
			return
		}
		iconPath = path
	})
	return iconPath, iconErr
}

func renderIcon() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, iconSize, iconSize))
	center := float64(iconSize) / 2
	radius := 28.0
	for y := 0; y < iconSize; y++ {
		for x := 0; x < iconSize; x++ {
			dx := float64(x) + 0.5 - center
			dy := float64(y) + 0.5 - center
			d := dx*dx + dy*dy
			if d > radius*radius {
				continue
			}
			alpha := uint8(255)
			if d > (radius-1)*(radius-1) {
				alpha = 180
			}
			img.SetNRGBA(x, y, color.NRGBA{R: 37, G: 99, B: 235, A: alpha})
		}
	}
	// 中央画一个简单的白色闪电.
	bolt := [][2]int{
		{36, 16}, {26, 34}, {32, 34}, {28, 48}, {40, 30}, {34, 30}, {38, 16},
	}
	for _, p := range bolt {
		for dy := 0; dy < 2; dy++ {
			for dx := 0; dx < 2; dx++ {
				x, y := p[0]+dx, p[1]+dy
				if x >= 0 && x < iconSize && y >= 0 && y < iconSize {
					img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
				}
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}
