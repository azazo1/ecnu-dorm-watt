"""生成通知图标的 SVG 源图和抗锯齿 PNG. 依赖 Pillow."""

from pathlib import Path
from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parent.parent
OUT = ROOT / "internal/client/notify/assets"
SIZE = 1024
SCALE = SIZE / 64
WHITE = "#f4f8ff"
AMBER = "#ffcc66"
BOLT = [(34.5, 24), (25.5, 35), (31.5, 35), (29, 44), (39, 31), (33, 31)]


def bounds(box):
    return tuple(round(v * SCALE) for v in box)


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    image = Image.new("RGBA", (SIZE, SIZE))
    pixels = image.load()
    top, bottom = (25, 43, 78), (33, 87, 119)
    for y in range(SIZE):
        t = min(1, max(0, (y / SCALE - 4.5) / 55))
        color = tuple(round(a + (b - a) * t) for a, b in zip(top, bottom))
        for x in range(SIZE):
            pixels[x, y] = (*color, 255)
    mask = Image.new("L", (SIZE, SIZE))
    ImageDraw.Draw(mask).rounded_rectangle(bounds((4.5, 4.5, 59.5, 59.5)), radius=13 * SCALE, fill=255)
    image.putalpha(mask)
    draw = ImageDraw.Draw(image)
    draw.rounded_rectangle(bounds((28, 14.5, 36, 18)), radius=1.5 * SCALE, fill=WHITE)
    draw.rounded_rectangle(bounds((20, 20.5, 44, 49.5)), radius=5 * SCALE, outline=WHITE, width=round(2.5 * SCALE))
    draw.polygon([(round(x * SCALE), round(y * SCALE)) for x, y in BOLT], fill=AMBER)
    image = image.resize((256, 256), Image.Resampling.LANCZOS)
    image.save(OUT / "notification.png")
    points = " ".join(f"{x},{y}" for x, y in BOLT)
    (OUT / "notification.svg").write_text(f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">
  <title>DormWatt</title>
  <defs>
    <linearGradient id="background" x1="0" y1="0" x2="0" y2="1">
      <stop stop-color="#192b4e"/>
      <stop offset="1" stop-color="#215777"/>
    </linearGradient>
  </defs>
  <rect x="4.5" y="4.5" width="55" height="55" rx="13" fill="url(#background)"/>
  <rect x="28" y="14.5" width="8" height="3.5" rx="1.5" fill="{WHITE}"/>
  <rect x="21.25" y="21.75" width="21.5" height="26.5" rx="3.75" fill="none" stroke="{WHITE}" stroke-width="2.5"/>
  <polygon points="{points}" fill="{AMBER}"/>
</svg>
''', encoding="utf-8")
    # 预览展示实际小尺寸与浅色, 深色背景.
    preview = Image.new("RGB", (720, 400), "#f3f5f9")
    draw = ImageDraw.Draw(preview)
    draw.rectangle((360, 0, 719, 399), fill="#111c2c")
    for left in (0, 360):
        preview.paste(image, (left + 52, 20), image)
        for x, size in ((116, 64), (204, 32), (260, 16)):
            small = image.resize((size, size), Image.Resampling.LANCZOS)
            preview.paste(small, (left + x, 310 + (64 - size) // 2), small)
    preview_dir = ROOT / ".tmp/icon-redesign"
    preview_dir.mkdir(parents=True, exist_ok=True)
    preview.save(preview_dir / "preview.png")
    print("已生成通知图标与大小尺寸预览")


if __name__ == "__main__":
    main()
