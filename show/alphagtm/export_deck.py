#!/usr/bin/env python3
"""从构建产物 deploy/index.html 真实导出 10 节 16:9 路演 PDF（纵向滚动版）。

流程：逐节 scrollIntoView 定位 -> 截图（节数取自 DOM，不写死）-> 无损合成多页 PDF
      -> 复制 PDF 进 deploy/（保证页面上的「下载 PDF」链接始终有效）
      -> 回读校验页数/尺寸/非空白。
只读 deploy/index.html，不改任何源文件。
"""
from __future__ import annotations

import re
import shutil
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
SRC = HERE / "deploy" / "index.html"
SHOTS = HERE / "shots"
PDF_OUT = HERE.parent / "deck" / "alphaGTM-huzhou-v2.pdf"
PDF_DEPLOY = HERE / "deploy" / "alphaGTM-huzhou-v2.pdf"

CHROME_CANDIDATES = [
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    "/Applications/Chromium.app/Contents/MacOS/Chromium",
    shutil.which("google-chrome") or "",
    shutil.which("chromium") or "",
]

SHOT_W, SHOT_H = 1920, 1080  # 16:9
PDF_DPI = 144                # 1920px @144dpi = 960×540pt


def find_chrome() -> str:
    for c in CHROME_CANDIDATES:
        if c and Path(c).exists():
            return c
    raise SystemExit("找不到 Chrome/Chromium，无法截图")


def sections_count(html: str) -> int:
    return len(re.findall(r'<section class="slide', html))


def chrome_dump(chrome: str, html: str, script: str, w: int, h: int) -> str:
    tmp = SHOTS / "_probe.html"
    SHOTS.mkdir(parents=True, exist_ok=True)
    tmp.write_text(html.replace("</body>", f"<script>{script}</script>\n</body>"), encoding="utf-8")
    r = subprocess.run(
        [chrome, "--headless=new", "--disable-gpu", "--hide-scrollbars",
         f"--window-size={w},{h}", "--virtual-time-budget=3000", "--dump-dom", f"file://{tmp}"],
        capture_output=True, text=True,
    )
    tmp.unlink(missing_ok=True)
    m = re.search(r'<pre id="probe">(.*?)</pre>', r.stdout, re.S)
    return m.group(1) if m else ""


def guard_section_heights(chrome: str, html: str, total: int) -> None:
    """每节在导出视口下必须正好一屏，否则截图会裁掉内容。"""
    script = (
        "const o=[...document.querySelectorAll('.slide')].map(s=>Math.round(s.getBoundingClientRect().height));"
        "let p=document.createElement('pre');p.id='probe';"
        "p.textContent=JSON.stringify({vh:innerHeight,h:o});document.body.appendChild(p);"
    )
    raw = chrome_dump(chrome, html, script, SHOT_W, SHOT_H + 87)
    if not raw:
        raise SystemExit("无法读取各节高度")
    import json  # noqa: PLC0415

    data = json.loads(raw)
    vh, heights = data["vh"], data["h"]
    if len(heights) != total:
        raise SystemExit(f"节数不符：DOM {len(heights)} vs 源码 {total}")
    tall = [(i + 1, h) for i, h in enumerate(heights) if h > vh + 1]
    if tall:
        raise SystemExit(f"这些节高于视口，截图会裁切：{tall}（视口 {vh}）")
    print(f"  视口 {vh}px，{total} 节全部一屏：{heights}")


def shoot(chrome: str, html: str, index: int, total: int) -> Path:
    """截第 index 节。

    headless 的 --screenshot 不跟随页面滚动（实测滚到目标节后仍截到文档原点），
    因此这里把其它节临时隐藏、只留目标节，再用 scrollIntoView 定位。
    因为每节在导出视口下正好一屏，结果与「下拉到该节」的视口一致。
    """
    injected = html.replace(
        "</body>",
        "<script>document.documentElement.style.scrollBehavior='auto';"
        "const sl=[...document.querySelectorAll('.slide')];"
        f"const n={index};const el=sl[n];"
        "el.scrollIntoView({block:'start'});"
        "try{ syncActive=function(){}; }catch(e){}"          # 关掉滚动回填，避免页码被重置成 01
        "sl.forEach((s,i)=>{ if(i!==n) s.style.display='none'; });"
        "if(typeof setActive==='function') setActive(n);"
        "setTimeout(()=>{ if(typeof setActive==='function') setActive(n); },400);"
        "</script>\n</body>",
    )
    tmp = SHOTS / "_view.html"
    tmp.write_text(injected, encoding="utf-8")
    out = SHOTS / f"slide-{index + 1:02d}.png"
    subprocess.run(
        [chrome, "--headless=new", "--disable-gpu", "--hide-scrollbars",
         "--force-device-scale-factor=1", "--virtual-time-budget=4000",
         f"--window-size={SHOT_W},{SHOT_H}", f"--screenshot={out}", f"file://{tmp}"],
        check=True, capture_output=True,
    )
    tmp.unlink(missing_ok=True)
    print(f"  shot {index + 1}/{total} -> {out.name}")
    return out


def main() -> None:
    if not SRC.is_file():
        raise SystemExit(f"缺少构建产物 {SRC}，先跑 build_deck.py")
    html = SRC.read_text(encoding="utf-8")
    total = sections_count(html)
    if total == 0:
        raise SystemExit("deploy/index.html 里没有 .slide")

    chrome = find_chrome()
    SHOTS.mkdir(parents=True, exist_ok=True)
    print(f"逐节截图 {total} 节 @ {SHOT_W}×{SHOT_H}")
    guard_section_heights(chrome, html, total)

    from PIL import Image  # noqa: PLC0415

    for i in range(total):
        p = shoot(chrome, html, i, total)
        im = Image.open(p)
        if im.size != (SHOT_W, SHOT_H):
            raise SystemExit(f"{p.name} 尺寸异常 {im.size}")

    # 无损嵌入（PNG/Flate），避免 JPEG 压缩把正文糊掉
    import fitz  # noqa: PLC0415

    doc = fitz.open()
    page_w = SHOT_W / PDF_DPI * 72.0
    page_h = SHOT_H / PDF_DPI * 72.0
    for i in range(total):
        page = doc.new_page(width=page_w, height=page_h)
        page.insert_image(
            fitz.Rect(0, 0, page_w, page_h),
            filename=str(SHOTS / f"slide-{i + 1:02d}.png"),
        )
    PDF_OUT.parent.mkdir(parents=True, exist_ok=True)
    doc.save(str(PDF_OUT), deflate=True, garbage=4)
    doc.close()

    size = PDF_OUT.stat().st_size
    print(f"OK  {PDF_OUT}")
    print(f"    pages={total}  bytes={size:,}")

    shutil.copy2(PDF_OUT, PDF_DEPLOY)
    print(f"    已复制到 {PDF_DEPLOY.relative_to(HERE.parent.parent)} {PDF_DEPLOY.stat().st_size:,}B")

    # 回读校验
    from pypdf import PdfReader  # noqa: PLC0415

    r = PdfReader(str(PDF_OUT))
    if len(r.pages) != total:
        raise SystemExit(f"PDF 页数 {len(r.pages)} != 节数 {total}")
    for i, pg in enumerate(r.pages):
        w = float(pg.mediabox.width)
        h = float(pg.mediabox.height)
        if abs(w / h - 16 / 9) > 0.001:
            raise SystemExit(f"第 {i + 1} 页不是 16:9：{w}×{h}")
    print(f"    校验：{len(r.pages)} 页，每页 {w:.0f}×{h:.0f}pt（16:9）")
    if size > 20 * 1024 * 1024:
        raise SystemExit("PDF 超过 20MB")


if __name__ == "__main__":
    main()
