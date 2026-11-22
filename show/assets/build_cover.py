#!/usr/bin/env python3
"""把 cover-src.html 渲染成作品广场封面 alphaGTM-cover.png（1600×900，<8MB）。

只读 cover-src.html 与其引用的 logo/字体，不改动任何源文件。
"""
from __future__ import annotations

import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
SRC = HERE / "cover-src.html"
OUT = HERE / "alphaGTM-cover.png"
W, H = 1600, 900
MAX_BYTES = 8 * 1024 * 1024

CHROME_CANDIDATES = [
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    "/Applications/Chromium.app/Contents/MacOS/Chromium",
]


def main() -> None:
    if not SRC.is_file():
        raise SystemExit(f"缺少 {SRC}")
    chrome = next((c for c in CHROME_CANDIDATES if Path(c).exists()), None)
    if not chrome:
        raise SystemExit("找不到 Chrome")

    subprocess.run(
        [
            chrome, "--headless=new", "--disable-gpu", "--hide-scrollbars",
            "--force-device-scale-factor=1", "--virtual-time-budget=4000",
            f"--window-size={W},{H}", f"--screenshot={OUT}", f"file://{SRC}",
        ],
        check=True, capture_output=True,
    )

    from PIL import Image  # noqa: PLC0415

    im = Image.open(OUT)
    size = OUT.stat().st_size
    print(f"OK  {OUT}")
    print(f"    {im.size[0]}×{im.size[1]} {im.mode}  {size:,} bytes")

    if im.size != (W, H):
        raise SystemExit(f"尺寸不是 {W}×{H}：{im.size}")
    if size >= MAX_BYTES:
        raise SystemExit(f"超过 8MB：{size}")


if __name__ == "__main__":
    main()
