#!/usr/bin/env python3
"""Render key product catalog pages from official Zhejiang Kooying PDFs into ui-web/public/guanying-catalog."""

from pathlib import Path
import fitz

ROOT = Path(__file__).resolve().parent.parent
MATERIALS = ROOT / "hackathon" / "materials"
OUT_DIRS = [
    ROOT / "ui-web" / "public" / "guanying-catalog",
    ROOT / "internal" / "ui" / "dist" / "guanying-catalog",
]

PAGES_TO_RENDER = [
    ("kooying catalog.pdf", 5, "kooying_eu_p6.png"),      # GYS-001~030 European / BPW / SAF / VOLVO / STEYR / HOWO
    ("kooying catalog.pdf", 9, "kooying_asia_p10.png"),   # GYS-119~146 HINO / STEYR / HOWO / EQ153 / HYUNDAI / 307AF / 311AF
    ("kooying catalog.pdf", 11, "kooying_faw_tata_p12.png"), # GYS-177~191 & EQ1060~1080
    ("kooying catalog.pdf", 14, "kooying_jiefang_p15.png"), # 解放悍威/奥威/天威
    ("kooying catalog.pdf", 24, "kooying_others_p25.png"),  # 19553 / 19932斯堪尼亚 / 三一重工 / 东风天锦
    ("冠隆产品册.pdf", 14, "guanlong_bus_axle_p15.png"),  # 金龙/宇通/海格/徐重50T/美驰桥
    ("冠隆产品册.pdf", 18, "guanlong_hande_p19.png"),     # 汉德桥35T/潍柴矿车/安桥440T/GY系列
    ("冠隆产品册.pdf", 20, "guanlong_disc_p21.png"),      # 盘式刹车片 29254/DA05/DA06/WVA29030/29087
    ("冠隆产品册.pdf", 21, "guanlong_disc_p22.png"),      # 盘式刹车片 WVA29090~29162
]


def main() -> None:
    for out_dir in OUT_DIRS:
        out_dir.mkdir(parents=True, exist_ok=True)

    docs = {}
    for pdf_name, page_idx, out_name in PAGES_TO_RENDER:
        if pdf_name not in docs:
            docs[pdf_name] = fitz.open(MATERIALS / pdf_name)
        doc = docs[pdf_name]
        if page_idx >= len(doc):
            continue
        pix = doc[page_idx].get_pixmap(dpi=135)
        for out_dir in OUT_DIRS:
            pix.save(str(out_dir / out_name))
        print(f"rendered {pdf_name} p{page_idx+1} -> {out_name} ({pix.width}x{pix.height})")


if __name__ == "__main__":
    main()
