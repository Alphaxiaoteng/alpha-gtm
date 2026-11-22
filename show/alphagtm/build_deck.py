#!/usr/bin/env python3
"""从 deck-src.html + assets 生成 deploy/index.html（纵向滚动版，10 节）。

占位符约定：
  __FONT__  Pinyon Script woff2 裸 base64（模板已带 data:font/woff2;base64,）
  __LOGO__  AlphaFDE 透明 PNG 裸 base64（模板已带 data:image/png;base64,）
  __ENP__ / __IDP__ / __FRP__   视频封面 JPEG，替换为完整 data:image/jpeg;base64,...
  __ENV__ / __IDV__ / __FRV__   竖屏 MP4，替换为完整 data:video/mp4;base64,...
  __PSHOT_*__  Harness 真实界面截图 JPEG，替换为完整 data:image/jpeg;base64,...

另：把已导出的路演 PDF 复制进 deploy/，保证「下载 PDF」链接不因重新构建而失效。
只替换占位符与复制文件，不改页面结构、文案与交互。
"""
from __future__ import annotations

import base64
import re
import shutil
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
SRC = HERE / "deck-src.html"
OUT = HERE / "deploy" / "index.html"
ASSETS = HERE / "assets"
SHOTS = HERE / "shots"
PDF_SRC = HERE.parent / "deck" / "alphaGTM-huzhou-v2.pdf"
PDF_DST = HERE / "deploy" / "alphaGTM-huzhou-v2.pdf"

JPG = "data:image/jpeg;base64,"
MP4 = "data:video/mp4;base64,"

FILES = {
    "__FONT__": (ASSETS / "fonts" / "pinyon-script.woff2", ""),
    "__LOGO__": (ASSETS / "alphafde-logo-710.png", ""),
    "__ENP__": (ASSETS / "sophia-workshop-en-local-clean-web-poster.jpg", JPG),
    "__ENV__": (ASSETS / "sophia-workshop-en-local-clean-web.mp4", MP4),
    "__IDP__": (ASSETS / "sophia-workshop-id-local-clean-web-poster.jpg", JPG),
    "__IDV__": (ASSETS / "sophia-workshop-id-local-clean-web.mp4", MP4),
    "__FRP__": (ASSETS / "sophia-workshop-fr-local-clean-web-poster.jpg", JPG),
    "__FRV__": (ASSETS / "sophia-workshop-fr-local-clean-web.mp4", MP4),
    # 本机 Alpha-Nexus EgoLite 真实界面截图（localhost:5200，2026-11-22）
    "__PSHOT_FEISHU__": (SHOTS / "shot-feishu.jpg", JPG),
    "__PSHOT_BOT__": (SHOTS / "shot-bot.jpg", JPG),
    # 外部只读实测截图（tiktok.com / www.kooying.com，2026-11-22）
    "__PSHOT_TIKTOK1__": (SHOTS / "shot-tiktok-autoparts.jpg", JPG),
    "__PSHOT_TIKTOK2__": (SHOTS / "shot-tiktok-brakefact.jpg", JPG),
    "__PSHOT_FOOTER__": (SHOTS / "shot-kooying-footer.jpg", JPG),
    # 解决方案二维码（解码值 https://alphagtm.pages.dev）
    "__QR__": (HERE.parent / "poster" / "alphagtm-qr.png", "data:image/png;base64,"),
    # 体验地址二维码（解码值 https://modelscope.cn/studios/cp1024/Alpha_gtm）
    "__QRDEMO__": (SHOTS / "qr-demo-modelscope.png", "data:image/png;base64,"),
}

EXPECT_SECTIONS = 11  # 封面 + 痛点 + 数据 + 媒体 + SEO + 成本 + 四层 + 五方案 + 技术栈 + Harness + 结尾
EXPECT_VIDEOS = 3
EXPECT_JPEG = 8  # 3 视频封面 + 5 真实截图（2 界面 + 3 媒体）
EXPECT_PNG = 3    # logo + 2 个二维码


def fail(msg: str) -> None:
    print("BUILD FAILED:", msg, file=sys.stderr)
    raise SystemExit(1)


def main() -> None:
    if not SRC.is_file():
        fail(f"missing source {SRC}")

    missing = [str(p) for p, _ in FILES.values() if not p.is_file()]
    if missing:
        fail("missing assets:\n  " + "\n  ".join(missing))

    html = SRC.read_text(encoding="utf-8")

    for token, (path, prefix) in FILES.items():
        if html.count(token) != 1:
            fail(f"{token} must appear exactly once, found {html.count(token)}")
        payload = base64.b64encode(path.read_bytes()).decode("ascii")
        html = html.replace(token, prefix + payload)

    left = sorted(set(re.findall(r"__[A-Z_]+__", html)))
    if left:
        fail(f"unreplaced placeholders: {left}")

    sections = len(re.findall(r'<section class="slide', html))
    if sections != EXPECT_SECTIONS:
        fail(f"expected {EXPECT_SECTIONS} sections, found {sections}")

    counts = {
        "data:video/mp4;base64,": EXPECT_VIDEOS,
        "data:image/jpeg;base64,": EXPECT_JPEG,
        "data:image/png;base64,": EXPECT_PNG,
        "data:font/woff2;base64,": 1,
    }
    for needle, want in counts.items():
        got = html.count(needle)
        if got != want:
            fail(f"{needle} expected {want}, found {got}")

    bare = re.search(r'(?:src|poster)="[A-Za-z0-9+/=]{200,}', html)
    if bare:
        fail("found attribute with bare base64 payload: " + bare.group(0)[:60])

    if 'href="./alphaGTM-huzhou-v2.pdf"' not in html:
        fail("缺少 PDF 下载链接 ./alphaGTM-huzhou-v2.pdf")

    OUT.parent.mkdir(parents=True, exist_ok=True)
    OUT.write_text(html, encoding="utf-8")

    pdf_note = "缺失（先跑 export_deck.py）"
    if PDF_SRC.is_file():
        shutil.copy2(PDF_SRC, PDF_DST)
        pdf_note = f"已复制 {PDF_DST.name} {PDF_DST.stat().st_size:,}B"
    else:
        print(f"WARN 未找到 {PDF_SRC}，deploy/ 里没有 PDF 可下载", file=sys.stderr)

    print(f"OK  {OUT.relative_to(HERE.parent.parent)}")
    print(f"    sections={sections}  bytes={OUT.stat().st_size:,}")
    print(f"    video={EXPECT_VIDEOS}  jpeg={EXPECT_JPEG}  png={EXPECT_PNG}  font=1  placeholders=0")
    print(f"    PDF  {pdf_note}")


if __name__ == "__main__":
    main()
