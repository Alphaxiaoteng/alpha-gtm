#!/usr/bin/env python3
"""易拉宝构建：重生成二维码 -> 内联进 poster.html -> 导出单页 PDF + PNG。

产物：alphagtm-qr.png（可扫）、poster.html（二维码内联）、
      alphaGTM-poster.pdf（1 页 80×200cm）、alphaGTM-poster.png（4724×11811）。
只读/只改 show/poster/ 内的文件，不碰客户原片。
"""
from __future__ import annotations

import base64
import re
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
HTML = HERE / "poster.html"
QR_PNG = HERE / "alphagtm-qr.png"
PDF = HERE / "alphaGTM-poster.pdf"
PNG = HERE / "alphaGTM-poster.png"

URL = "https://alphagtm.pages.dev"
PAGE_PT = (2267.72, 5669.29)  # 80cm × 200cm
PNG_SIZE = (4724, 11811)

CHROME = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"


def make_qr() -> None:
    import qrcode  # noqa: PLC0415
    from qrcode.constants import ERROR_CORRECT_H  # noqa: PLC0415

    qr = qrcode.QRCode(error_correction=ERROR_CORRECT_H, box_size=30, border=4)
    qr.add_data(URL)
    qr.make(fit=True)
    img = qr.make_image(fill_color="#0A0A0A", back_color="#FFFFFF").convert("RGB")
    img.save(QR_PNG)
    print(f"  QR  {QR_PNG.name} {img.size} -> {URL}")


def inline_qr() -> None:
    html = HTML.read_text(encoding="utf-8")
    data_uri = "data:image/png;base64," + base64.b64encode(QR_PNG.read_bytes()).decode("ascii")
    new_tag = f'<img class="qr" src="{data_uri}" alt="alphagtm.pages.dev">'
    pat = re.compile(r'<img class="qr" src="[^"]*"[^>]*>')
    if len(pat.findall(html)) != 1:
        raise SystemExit("poster.html 里 .qr 的 img 不是恰好 1 个")
    html2 = pat.sub(lambda _: new_tag, html)
    if html2 == html and "data:image/png;base64," not in html:
        raise SystemExit("二维码内联失败")
    HTML.write_text(html2, encoding="utf-8")
    print(f"  内联 QR -> poster.html（{len(data_uri):,} 字符）")


def export_pdf() -> None:
    """用 4724×11811 的 PNG 合成精确 80×200cm 单页 PDF。

    不走 Chrome --print-to-pdf：它会按页宽自动缩放再分页，导致 4724px 内容溢出到第 2 页。
    """
    import fitz  # noqa: PLC0415

    doc = fitz.open()
    page = doc.new_page(width=PAGE_PT[0], height=PAGE_PT[1])
    page.insert_image(fitz.Rect(0, 0, PAGE_PT[0], PAGE_PT[1]), filename=str(PNG))
    doc.save(str(PDF), deflate=True, garbage=4)
    doc.close()


def export_png() -> None:
    subprocess.run(
        [
            CHROME, "--headless=new", "--disable-gpu", "--hide-scrollbars",
            "--force-device-scale-factor=1", "--virtual-time-budget=8000",
            f"--screenshot={PNG}", f"--window-size={PNG_SIZE[0]},{PNG_SIZE[1]}",
            f"file://{HTML}",
        ],
        check=True, capture_output=True,
    )


def verify() -> None:
    from PIL import Image  # noqa: PLC0415
    from pypdf import PdfReader  # noqa: PLC0415

    r = PdfReader(str(PDF))
    pages = len(r.pages)
    w = float(r.pages[0].mediabox.width)
    h = float(r.pages[0].mediabox.height)
    print(f"  PDF  {PDF.name} pages={pages} {w:.2f}×{h:.2f}pt ratio={w / h:.4f}")
    print(f"       bytes={PDF.stat().st_size:,}")
    if pages != 1:
        raise SystemExit(f"PDF 必须是 1 页，实际 {pages} 页")
    if abs(w / h - 0.4) > 0.002:
        raise SystemExit(f"不是 80×200cm 比例：{w / h:.4f}")

    im = Image.open(PNG)
    print(f"  PNG  {PNG.name} {im.size[0]}×{im.size[1]} bytes={PNG.stat().st_size:,}")
    if im.size != PNG_SIZE:
        raise SystemExit(f"PNG 尺寸异常 {im.size}")


def main() -> None:
    if not HTML.is_file():
        raise SystemExit(f"缺少 {HTML}")
    if not Path(CHROME).exists():
        raise SystemExit("找不到 Chrome")
    make_qr()
    inline_qr()
    export_png()
    export_pdf()
    verify()
    print("OK")


if __name__ == "__main__":
    main()
