from pathlib import Path
import shutil

root = Path(__file__).resolve().parent.parent
src = root / "web" / "dist"
dst = root / "internal" / "client" / "webui" / "dist"

if not src.is_dir():
    raise SystemExit(f"Web build output not found: {src}")

if dst.exists():
    shutil.rmtree(dst)
shutil.copytree(src, dst)
print(f"embedded web assets: {dst}")
