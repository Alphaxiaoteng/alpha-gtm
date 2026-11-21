#!/usr/bin/env python3
"""
Unpack Zhejiang KOOYING drum & disc brake pad photo archives cleanly (fixing GBK encoding
and deduplicating mirrored subdirectories), extract high-resolution factory/dyno/engineering
images from official PDFs, and build unified JSON/JS product catalog indexes for the brand website.
"""

from __future__ import annotations

import json
import re
import shutil
import zipfile
from pathlib import Path
from typing import Any

import fitz  # PyMuPDF

ROOT = Path(__file__).resolve().parent.parent
PROJECT_ROOT = ROOT.parent
MATERIALS_DIR = PROJECT_ROOT / "hackathon" / "materials"

DRUM_ZIP = Path("/Users/albert/Downloads/鼓式刹车片照片.zip")
DISC_ZIP = Path("/Users/albert/Downloads/盘式刹车片照片.zip")

DRUM_OUT = ROOT / "assets" / "products" / "drum"
DISC_OUT = ROOT / "assets" / "products" / "disc"
FACTORY_OUT = ROOT / "assets" / "factory"
DRAWINGS_OUT = ROOT / "assets" / "catalog-drawings"
DATA_OUT = ROOT / "assets" / "data"


def decode_zip_filename(raw_name: str) -> str:
    """Decode CP437-wrapped GBK filenames from Windows ZIP archives safely."""
    try:
        return raw_name.encode("cp437").decode("gbk")
    except Exception:
        try:
            return raw_name.encode("cp437").decode("utf-8")
        except Exception:
            return raw_name


def sanitize_rel_parts(parts: list[str]) -> list[str]:
    """Normalize trailing/leading whitespace in folder/file names while preserving model identity."""
    cleaned: list[str] = []
    for part in parts:
        p = part.strip().replace("\u3000", " ")
        # Replace consecutive spaces with single space
        p = re.sub(r"\s+", " ", p)
        if p:
            cleaned.append(p)
    return cleaned


def extract_drum_zip() -> dict[str, list[str]]:
    """Extract drum brake pad photos cleanly, deduplicating mirrored /鼓刹/ and /日野前后/日野后."""
    if DRUM_OUT.exists():
        shutil.rmtree(DRUM_OUT)
    DRUM_OUT.mkdir(parents=True, exist_ok=True)

    extracted: dict[str, list[str]] = {}
    with zipfile.ZipFile(DRUM_ZIP, "r") as zf:
        for info in zf.infolist():
            if info.is_dir():
                continue
            decoded = decode_zip_filename(info.filename)
            # Skip mirrored /鼓刹/ subtree and macOS metadata
            if "/鼓刹/" in decoded or "__MACOSX" in decoded or ".DS_Store" in decoded:
                continue
            rel = decoded.split("鼓式刹车片照片/", 1)[-1]
            # Deduplicate 日野前后/日野后 because top-level 日野后 already holds identical files
            if rel.startswith("日野前后/日野后/"):
                continue
            parts = sanitize_rel_parts(rel.split("/"))
            if not parts:
                continue
            # Flatten 日野前后/日野前 -> 日野前
            if len(parts) >= 2 and parts[0] == "日野前后":
                parts = parts[1:]

            model_dir = parts[0]
            target_path = DRUM_OUT.joinpath(*parts)
            target_path.parent.mkdir(parents=True, exist_ok=True)
            with zf.open(info, "r") as src, open(target_path, "wb") as dst:
                shutil.copyfileobj(src, dst)

            rel_web = f"assets/products/drum/{'/'.join(parts)}"
            extracted.setdefault(model_dir, []).append(rel_web)

    return extracted


def extract_disc_zip() -> tuple[dict[str, list[str]], list[dict[str, str]], list[str], list[str]]:
    """
    Extract disc brake pad photos cleanly:
    - Model folders -> assets/products/disc/<model>/<file>
    - 5-brand 29162 renders -> assets/products/disc/brand-renders/
    - Sample / ink-jet templates & loose gallery -> assets/products/disc/samples/
    - Video: keep 5200.5300.mp4, skip duplicate .MOV
    """
    if DISC_OUT.exists():
        shutil.rmtree(DISC_OUT)
    DISC_OUT.mkdir(parents=True, exist_ok=True)

    models: dict[str, list[str]] = {}
    brand_renders: list[dict[str, str]] = []
    samples: list[str] = []
    videos: list[str] = []

    with zipfile.ZipFile(DISC_ZIP, "r") as zf:
        for info in zf.infolist():
            if info.is_dir():
                continue
            decoded = decode_zip_filename(info.filename)
            if "__MACOSX" in decoded or ".DS_Store" in decoded:
                continue
            rel = decoded.split("盘式刹车片照片/", 1)[-1]
            # Skip duplicate .MOV because 视频改后缀/5200.5300.mp4 is identical MP4
            if rel.lower().endswith(".mov"):
                continue
            parts = sanitize_rel_parts(rel.split("/"))
            if not parts:
                continue

            # Brand renders
            if parts[0] == "冠英各品牌常规型号盘刹宣传图":
                brand_label = parts[1] if len(parts) > 2 else "Brand"
                fname = parts[-1]
                target = DISC_OUT / "brand-renders" / brand_label / fname
                target.parent.mkdir(parents=True, exist_ok=True)
                with zf.open(info, "r") as src, open(target, "wb") as dst:
                    shutil.copyfileobj(src, dst)
                web_path = f"assets/products/disc/brand-renders/{brand_label}/{fname}"
                brand_renders.append({"brand_folder": brand_label, "path": web_path})
                continue

            # Samples / ink-jet / loose root JPGs
            if parts[0] in ("样板", "盘刹喷码样板") or len(parts) == 1:
                sub = "samples" if len(parts) == 1 else parts[0]
                fname = parts[-1]
                target = DISC_OUT / "samples" / fname
                target.parent.mkdir(parents=True, exist_ok=True)
                with zf.open(info, "r") as src, open(target, "wb") as dst:
                    shutil.copyfileobj(src, dst)
                samples.append(f"assets/products/disc/samples/{fname}")
                continue

            # Standard disc model folder
            model_dir = parts[0]
            # Normalize 视频改后缀/5200.5300.mp4 -> 5200、5300/5200.5300.mp4
            if len(parts) == 3 and parts[1] == "视频改后缀":
                parts = [parts[0], parts[2]]

            target = DISC_OUT.joinpath(*parts)
            target.parent.mkdir(parents=True, exist_ok=True)
            with zf.open(info, "r") as src, open(target, "wb") as dst:
                shutil.copyfileobj(src, dst)

            web_path = f"assets/products/disc/{'/'.join(parts)}"
            if web_path.lower().endswith(".mp4"):
                videos.append(web_path)
            else:
                models.setdefault(model_dir, []).append(web_path)

    return models, brand_renders, samples, videos


def render_pdf_assets() -> dict[str, list[dict[str, str]]]:
    """Render key factory, R&D lab, certification, dyno test and catalog drawing pages from official PDFs."""
    FACTORY_OUT.mkdir(parents=True, exist_ok=True)
    DRAWINGS_OUT.mkdir(parents=True, exist_ok=True)

    intro_pdf = fitz.open(MATERIALS_DIR / "2026年浙江冠英企业介绍.pdf")
    kooying_pdf = fitz.open(MATERIALS_DIR / "kooying catalog.pdf")
    guanlong_pdf = fitz.open(MATERIALS_DIR / "冠隆产品册.pdf")

    factory_specs = [
        (intro_pdf, 3, "huzhou-plant-overview.jpg", "Huzhou South Taihu R&D & Manufacturing Base (RMB 180M Investment)"),
        (intro_pdf, 4, "auto-batching-mixing.jpg", "Automated Raw Material Batching & Mixing System"),
        (intro_pdf, 7, "automated-hot-press.jpg", "Temperature-Controlled Multi-Cavity Hot Pressing Line"),
        (intro_pdf, 8, "finishing-inspection-line.jpg", "Automated Grinding, Slotting & Laser Marking Line"),
        (intro_pdf, 9, "krauss-dyno-lab.jpg", "2x European Krauss & 4x Constant-Speed Testing Machines"),
        (intro_pdf, 10, "chase-shear-compress-lab.jpg", "North American Chase, Shear & Compressibility Lab Equipment"),
        (intro_pdf, 12, "iatf16949-3c-honors.jpg", "IATF 16949:2016, 3C & National High-Tech Enterprise Certifications"),
        (intro_pdf, 14, "university-mineral-fiber-rd.jpg", "Joint R&D with NUDT & China University of Geosciences on Nano-Mineral Fibers"),
        (intro_pdf, 23, "dyno-wear-400c-comparison.jpg", "200C / 300C / 400C+ Constant-Torque Wear Comparison (0.040mm vs 0.085mm)"),
        (intro_pdf, 24, "dyno-constant-speed-table.jpg", "Constant-Speed Friction & Impact Strength Benchmark Table"),
    ]

    drawing_specs = [
        (kooying_pdf, 5, "kooying-european-bpw-steyr-p6.jpg", "Kooying European & Axle Catalog (GYS-001~030, BPW, Steyr, Volvo, Mercedes)"),
        (kooying_pdf, 9, "kooying-asian-truck-bus-p10.jpg", "Kooying Japanese, Korean & Chinese Truck Catalog (GYS-119~146, HINO, HYUNDAI, 307AF)"),
        (kooying_pdf, 14, "kooying-jiefang-series-p15.jpg", "Kooying JIEFANG Heavy Truck Drilling Drawings (Aowei, Hanwei, Tianwei)"),
        (kooying_pdf, 24, "kooying-scania-19553-sany-p25.jpg", "Kooying 19553, 19932 SCANIA, SANY & Dongfeng Drilling Drawings"),
        (guanlong_pdf, 14, "guanlong-bus-meritor-axle-p15.jpg", "Guanlong Bus & Meritor Axle Specification Table"),
        (guanlong_pdf, 18, "guanlong-hande-weichai-axle-p19.jpg", "Guanlong Hande 35T & Weichai Mining Axle Specifications"),
        (guanlong_pdf, 20, "guanlong-disc-wva-29030-29087-p21.jpg", "Disc Brake Pad Engineering Drawings (WVA 29030, 29087, DA05, DA06, AD02, AD03)"),
        (guanlong_pdf, 21, "guanlong-disc-wva-29090-29162-p22.jpg", "Disc Brake Pad Engineering Drawings (WVA 29090~29162)"),
    ]

    rendered: dict[str, list[dict[str, str]]] = {"factory": [], "drawings": []}

    for doc, idx, fname, caption in factory_specs:
        if idx < len(doc):
            pix = doc[idx].get_pixmap(dpi=135)
            out_path = FACTORY_OUT / fname
            pix.save(str(out_path))
            rendered["factory"].append({
                "file": f"assets/factory/{fname}",
                "caption": caption,
                "width": str(pix.width),
                "height": str(pix.height),
            })

    for doc, idx, fname, caption in drawing_specs:
        if idx < len(doc):
            pix = doc[idx].get_pixmap(dpi=145)
            out_path = DRAWINGS_OUT / fname
            pix.save(str(out_path))
            rendered["drawings"].append({
                "file": f"assets/catalog-drawings/{fname}",
                "caption": caption,
                "width": str(pix.width),
                "height": str(pix.height),
            })

    # Also extract clean borderless embedded photographs from company intro PDF
    raw_dir = FACTORY_OUT / "raw"
    raw_dir.mkdir(parents=True, exist_ok=True)
    raw_xref_map = {
        106: ("plant-aerial.jpg", "Huzhou South Taihu Plant Aerial Render (41 Acres / 43,000 sqm)"),
        107: ("rd-center-building.jpg", "KOOYING Friction Materials R&D Center"),
        119: ("auto-batching-line.jpg", "Automated Raw Material Batching & Dosing Line"),
        130: ("hot-pressing-workshop.jpg", "Automated Temperature-Controlled Hot-Pressing Workshop"),
        135: ("finishing-grinding-line.jpg", "Automated Grinding, Slotting & Finishing Line"),
        146: ("krauss-machines-clean.jpg", "European Krauss Testing Machines (24/7 Batch Quality Control)"),
        173: ("iatf-16949-certificate.jpg", "IATF 16949:2016 Quality Management Certificate"),
        172: ("ccc-certificate.jpg", "China Compulsory Certification (3C)"),
        208: ("nudt-cug-research.jpg", "Joint Nano-Mineral Fiber & High-Carbon Friction Research"),
    }
    rendered["factory_clean"] = []
    for xref, (fname, caption) in raw_xref_map.items():
        try:
            img_data = intro_pdf.extract_image(xref)
            ext = img_data.get("ext", "jpg")
            out_name = Path(fname).with_suffix(f".{ext}").name
            out_path = raw_dir / out_name
            out_path.write_bytes(img_data["image"])
            rendered["factory_clean"].append({
                "file": f"assets/factory/raw/{out_name}",
                "caption": caption,
                "width": str(img_data.get("width", "")),
                "height": str(img_data.get("height", "")),
            })
        except Exception as exc:
            print(f"Warning: could not extract xref={xref}: {exc}")

    return rendered


# Curated engineering ground-truth specs directly verified from kooying catalog.pdf & 冠隆产品册.pdf
DISC_SPECS: dict[str, dict[str, Any]] = {
    "29087": {
        "wva": "WVA 29087",
        "gys": "GYD-29087",
        "size": "247.6 × 109.7 × 30.0",
        "length_mm": 247.6,
        "width_mm": 109.7,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "Knorr SB7 / SN7",
        "vehicles": "SCANIA, MAN, Mercedes-Benz, ZF Axle, Shacman, Yutong 22.5\", Steyr Front",
        "axle": "ZF / Knorr 22.5\"",
        "markets": ["Europe", "Middle East", "South America"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "29087加长": {
        "wva": "WVA 29087 Extended (24.5K)",
        "gys": "GYD-29087L",
        "size": "247.6 × 109.7 × 30.0",
        "length_mm": 247.6,
        "width_mm": 109.7,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "Knorr 24.5K Heavy Caliper",
        "vehicles": "Heavy-Duty 24.5K Extended Axle Trucks & Mining Trailers",
        "axle": "24.5K Heavy Axle",
        "markets": ["Middle East", "South America"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "29030": {
        "wva": "WVA 29030",
        "gys": "GYD-29030",
        "size": "249.5 × 118.2 × 30.0",
        "length_mm": 249.5,
        "width_mm": 118.2,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "Meritor Elsa / Knorr",
        "vehicles": "MAN Lion's Coach, Setra Kassbohrer, Foton Auman, Ankai-10 Bus",
        "axle": "Meritor / MAN Front & Rear",
        "markets": ["Europe", "Middle East"],
        "formula": "GY-CV360 (Low-Noise Fleet Formula)",
    },
    "29093": {
        "wva": "WVA 29093 / WG9093",
        "gys": "GYD-29093",
        "size": "210.5 × 92.5 × 30.0",
        "length_mm": 210.5,
        "width_mm": 92.5,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "Knorr SB6 / 19.5\"",
        "vehicles": "Neoplan 19.5\", MAN TGL/TGM, European Medium Truck",
        "axle": "19.5\" Air Disc Axle",
        "markets": ["Europe", "Middle East", "Southeast Asia"],
        "formula": "GY-CV360 (Low-Noise Fleet Formula)",
    },
    "29115，29148": {
        "wva": "WVA 29115 / 29148",
        "gys": "GYD-29115",
        "size": "247.5 × 109.5 × 30.0",
        "length_mm": 247.5,
        "width_mm": 109.5,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "Meritor ELSA 225",
        "vehicles": "Renault Magnum/Premium, Volvo FH/FM, DAF XF",
        "axle": "Meritor / Volvo 22.5\"",
        "markets": ["Europe", "South America", "Middle East"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "29125": {
        "wva": "WVA 29125",
        "gys": "GYD-29125",
        "size": "249.5 × 111.0 × 29.5",
        "length_mm": 249.5,
        "width_mm": 111.0,
        "thickness_mm": 29.5,
        "holes": 0,
        "caliper": "Meritor DX195 / Volvo",
        "vehicles": "Sunwin Volvo Bus (Euro II/III), Volvo B7R/B12B",
        "axle": "Volvo / Sunwin Coach Axle",
        "markets": ["Southeast Asia", "Middle East", "South America"],
        "formula": "GY-CV360 (Low-Noise Fleet Formula)",
    },
    "29126VIE19.5": {
        "wva": "WVA 29126 / VIE 19.5",
        "gys": "GYD-29126V",
        "size": "210.6 × 92.5 × 30.0",
        "length_mm": 210.6,
        "width_mm": 92.5,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "VIE 19.5\" / Meritor",
        "vehicles": "Volvo VM17/VM23, Neoplan 8m Coach, VIE 19.5\" Axle",
        "axle": "VIE 19.5\" / Volvo VM",
        "markets": ["South America", "Southeast Asia"],
        "formula": "GY-CV360 (Low-Noise Fleet Formula)",
    },
    "29131": {
        "wva": "WVA 29131",
        "gys": "GYD-29131",
        "size": "249.6 × 118.1 × 30.0",
        "length_mm": 249.6,
        "width_mm": 118.1,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "Meritor Classic / Knorr",
        "vehicles": "Meritor Classic Axle, Yutong ZK6126, Ankai K147, Neoplan Rear",
        "axle": "Meritor Classic",
        "markets": ["Middle East", "South America", "Southeast Asia"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "29143": {
        "wva": "WVA 29143",
        "gys": "GYD-29143",
        "size": "247.6 × 104.9 × 30.0",
        "length_mm": 247.6,
        "width_mm": 104.9,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "Dana / Knorr 22.5\"",
        "vehicles": "KingLong 6122 Coach, Dana Axle",
        "axle": "Dana Axle",
        "markets": ["Middle East", "Southeast Asia"],
        "formula": "GY-CV360 (Low-Noise Fleet Formula)",
    },
    "29162": {
        "wva": "WVA 29162",
        "gys": "GYD-29162",
        "size": "210.5 × 109.5 × 30.0",
        "length_mm": 210.5,
        "width_mm": 109.5,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "Wabco PAN19 / BPW",
        "vehicles": "BPW Trailer Axle, SAF, European 19.5\"–22.5\" Disc Trailers",
        "axle": "BPW / Wabco PAN19",
        "markets": ["Europe", "Middle East", "South America"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "29162加长": {
        "wva": "WVA 29162 Extended (217.5mm)",
        "gys": "GYD-29162L",
        "size": "217.5 × 107.7 × 30.0",
        "length_mm": 217.5,
        "width_mm": 107.7,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "Wabco PAN22 / BPW Heavy",
        "vehicles": "Extended 217.5mm Heavy Trailer Disc Axle (Kooying / Guanlong Flagship)",
        "axle": "BPW / Heavy Trailer 217.5mm",
        "markets": ["Middle East", "South America", "Southeast Asia"],
        "formula": "PGB P37 Ceramic / GY-HD400",
    },
    "29174": {
        "wva": "WVA 29174",
        "gys": "GYD-29174",
        "size": "247.5 × 109.5 × 30.0",
        "length_mm": 247.5,
        "width_mm": 109.5,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "Knorr SK7 / Volvo FH",
        "vehicles": "Volvo FH/FM12, Renault Magnum, Heavy Long-Haul Tractors",
        "axle": "Volvo / Knorr SK7",
        "markets": ["Europe", "North America", "South America"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "29181": {
        "wva": "WVA 29181",
        "gys": "GYD-29181",
        "size": "210.5 × 99.6 × 30.0",
        "length_mm": 210.5,
        "width_mm": 99.6,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "Wabco PAN 19-1 Plus",
        "vehicles": "SAF / Schmitz Cargobull / MAN TGM Trailers",
        "axle": "SAF / Wabco PAN19",
        "markets": ["Europe", "Middle East"],
        "formula": "GY-CV360 (Low-Noise Fleet Formula)",
    },
    "29187": {
        "wva": "WVA 29187",
        "gys": "GYD-29187",
        "size": "249.6 × 118.2 × 30.0",
        "length_mm": 249.6,
        "width_mm": 118.2,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "Meritor Elsa 2",
        "vehicles": "MAN TGA / TGX / DAF CF & XF Heavy Tractors",
        "axle": "MAN / DAF Heavy Axle",
        "markets": ["Europe", "Middle East", "South America"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "29228": {
        "wva": "WVA 29228 (BPW TSB)",
        "gys": "GYD-29228",
        "size": "210.4 × 109.8 × 30.0",
        "length_mm": 210.4,
        "width_mm": 109.8,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "BPW TSB 3709 / 4309",
        "vehicles": "BPW Disc Trailer Axles (ECO Disc TSB Series)",
        "axle": "BPW ECO Disc",
        "markets": ["Europe", "Middle East", "South America"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "29253": {
        "wva": "WVA 29253 / 29254",
        "gys": "GYD-29253",
        "size": "250.0 × 100.3 × 30.0",
        "length_mm": 250.0,
        "width_mm": 100.3,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "Wabco / Knorr Heavy",
        "vehicles": "SINOTRUK HOWO A7, Shacman Delong F3000/X3000",
        "axle": "HOWO / Delong Disc Axle",
        "markets": ["Southeast Asia", "Middle East", "South America"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "29267": {
        "wva": "WVA 29267 (8301)",
        "gys": "GYD-29267",
        "size": "210.4 × 108.0 × 30.0",
        "length_mm": 210.4,
        "width_mm": 108.0,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "BPW ECO Disc 8301",
        "vehicles": "BPW 8301 Disc Caliper Trailer Axles",
        "axle": "BPW 8301",
        "markets": ["Europe", "Middle East"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "29279": {
        "wva": "WVA 29279",
        "gys": "GYD-29279",
        "size": "244.5 × 114.6 × 30.0",
        "length_mm": 244.5,
        "width_mm": 114.6,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "Brembo / Knorr Next-Gen",
        "vehicles": "Mercedes Actros MP4, Iveco Stralis, European Tractors",
        "axle": "Actros MP4 / Iveco",
        "markets": ["Europe", "Middle East"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "29289": {
        "wva": "WVA 29289",
        "gys": "GYD-29289",
        "size": "210.5 × 109.5 × 30.0",
        "length_mm": 210.5,
        "width_mm": 109.5,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "Wabco PAN22-1 / SAF",
        "vehicles": "SAF SBS 2220 / Krone / Schmitz Trailer Axles",
        "axle": "SAF SBS 2220",
        "markets": ["Europe", "Middle East", "South America"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "29320": {
        "wva": "WVA 29320",
        "gys": "GYD-29320",
        "size": "247.5 × 109.5 × 30.0",
        "length_mm": 247.5,
        "width_mm": 109.5,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "Knorr ST7 / SCANIA Next-Gen",
        "vehicles": "SCANIA R/S Next-Gen, MAN TGX Euro 6 Heavy Line",
        "axle": "Knorr ST7",
        "markets": ["Europe", "South America", "Middle East"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "25K专用": {
        "wva": "25K Special Heavy",
        "gys": "GYD-25K",
        "size": "247.6 × 109.7 × 31.0",
        "length_mm": 247.6,
        "width_mm": 109.7,
        "thickness_mm": 31.0,
        "holes": 0,
        "caliper": "25K Heavy Trailer Caliper",
        "vehicles": "25K Heavy-Load Trailer Disc Axle & Long-Downhill Fleets",
        "axle": "25K Heavy Disc Axle",
        "markets": ["Middle East", "South America"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "BPW8201": {
        "wva": "BPW 8201 / WVA 29228 Alt",
        "gys": "GYD-8201",
        "size": "210.4 × 109.8 × 30.0",
        "length_mm": 210.4,
        "width_mm": 109.8,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "BPW 8201 ECO Disc",
        "vehicles": "BPW 8201 Semi-Trailer & Tanker Axles",
        "axle": "BPW 8201",
        "markets": ["Europe", "Middle East", "South America"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "AD01": {
        "wva": "AD01",
        "gys": "GYD-AD01",
        "size": "239.5 × 111.2 × 30.0",
        "length_mm": 239.5,
        "width_mm": 111.2,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "22.5\" Coach & Truck",
        "vehicles": "KingLong / Higer 22.5\" Heavy Coach & Tractor Front",
        "axle": "Coach 22.5\" Front",
        "markets": ["Southeast Asia", "Middle East"],
        "formula": "GY-CV360 (Low-Noise Fleet Formula)",
    },
    "AD02": {
        "wva": "AD02",
        "gys": "GYD-AD02",
        "size": "209.5 × 89.5 × 29.0",
        "length_mm": 209.5,
        "width_mm": 89.5,
        "thickness_mm": 29.0,
        "holes": 0,
        "caliper": "19.5\" City Bus Caliper",
        "vehicles": "KingLong 6820, Higer 19.5\" City Bus & Transit Fleets",
        "axle": "19.5\" City Bus",
        "markets": ["Southeast Asia", "South America"],
        "formula": "GY-CV360 (Low-Noise Fleet Formula)",
    },
    "AD03": {
        "wva": "AD03",
        "gys": "GYD-AD03",
        "size": "239.5 × 111.2 × 30.0",
        "length_mm": 239.5,
        "width_mm": 111.2,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "22.5\" Bus & Dongfeng",
        "vehicles": "KingLong Large Coach, Dongfeng 22.5\" Disc Truck",
        "axle": "Dongfeng / Coach 22.5\"",
        "markets": ["Southeast Asia", "Middle East"],
        "formula": "GY-CV360 (Low-Noise Fleet Formula)",
    },
    "AD04": {
        "wva": "AD04",
        "gys": "GYD-AD04",
        "size": "220.0 × 99.5 × 29.0",
        "length_mm": 220.0,
        "width_mm": 99.5,
        "thickness_mm": 29.0,
        "holes": 0,
        "caliper": "19.5\" / 20\" Commercial",
        "vehicles": "Yutong / KingLong Transit & Medium Truck Disc Axle",
        "axle": "Transit Disc Axle",
        "markets": ["Southeast Asia", "South America"],
        "formula": "GY-CV360 (Low-Noise Fleet Formula)",
    },
    "DA06": {
        "wva": "DA06",
        "gys": "GYD-DA06",
        "size": "223.9 × 103.2 × 29.0",
        "length_mm": 223.9,
        "width_mm": 103.2,
        "thickness_mm": 29.0,
        "holes": 0,
        "caliper": "Dongfeng Tianjin Caliper",
        "vehicles": "Dongfeng Tianjin (DFH) Medium-Heavy Trucks",
        "axle": "Dongfeng Front Disc",
        "markets": ["Southeast Asia", "South America", "Middle East"],
        "formula": "GY-CV360 (Low-Noise Fleet Formula)",
    },
    "DA20": {
        "wva": "DA20",
        "gys": "GYD-DA20",
        "size": "247.5 × 109.5 × 30.0",
        "length_mm": 247.5,
        "width_mm": 109.5,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "Dongfeng Heavy 22.5\"",
        "vehicles": "Dongfeng Kinland / Heavy Tractor 22.5\" Disc Axle",
        "axle": "Dongfeng Heavy 22.5\"",
        "markets": ["Southeast Asia", "Middle East", "South America"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "5200、5300": {
        "wva": "5200 / 5300 Series (TYX-028)",
        "gys": "GYD-5200",
        "size": "217.0 × 108.0 × 31.0",
        "length_mm": 217.0,
        "width_mm": 108.0,
        "thickness_mm": 31.0,
        "holes": 0,
        "caliper": "Fuwa Trailer / Knorr 20\" Disc",
        "vehicles": "Fuwa Trailer Disc Axle & Knorr 20\" Heavy Commercial Assembly",
        "axle": "Fuwa / Knorr 20\" Disc",
        "markets": ["Middle East", "South America", "Southeast Asia"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "VIE19.5": {
        "wva": "VIE 19.5\"",
        "gys": "GYD-VIE195",
        "size": "210.5 × 92.5 × 29.5",
        "length_mm": 210.5,
        "width_mm": 92.5,
        "thickness_mm": 29.5,
        "holes": 0,
        "caliper": "VIE WanAn 19.5\"",
        "vehicles": "VIE 19.5\" City Bus & Express Trailer Caliper",
        "axle": "VIE 19.5\"",
        "markets": ["Southeast Asia", "Middle East"],
        "formula": "GY-CV360 (Low-Noise Fleet Formula)",
    },
    "VIE20K": {
        "wva": "VIE 20K",
        "gys": "GYD-VIE20K",
        "size": "224.0 × 104.0 × 30.0",
        "length_mm": 224.0,
        "width_mm": 104.0,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "VIE WanAn 20K",
        "vehicles": "VIE 20K Heavy Trailer & Transit Bus Axle",
        "axle": "VIE 20K",
        "markets": ["Southeast Asia", "Middle East", "South America"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "WG443050": {
        "wva": "WG443050",
        "gys": "GYD-WG443",
        "size": "247.6 × 109.7 × 30.0",
        "length_mm": 247.6,
        "width_mm": 109.7,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "SINOTRUK / Shacman",
        "vehicles": "SINOTRUK HOWO T7H / SITRAK Disc Front Axle",
        "axle": "HOWO / SITRAK",
        "markets": ["Southeast Asia", "Middle East", "South America"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "WG443050冲钉": {
        "wva": "WG443050 Riveted Pin",
        "gys": "GYD-WG443P",
        "size": "247.6 × 109.7 × 30.0",
        "length_mm": 247.6,
        "width_mm": 109.7,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "SINOTRUK / Reinforced Pin",
        "vehicles": "SINOTRUK SITRAK Heavy-Duty Mining & Overloaded Fleets",
        "axle": "SITRAK Heavy Front",
        "markets": ["Middle East", "South America"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "WG444150": {
        "wva": "WG444150",
        "gys": "GYD-WG444",
        "size": "249.5 × 114.5 × 30.0",
        "length_mm": 249.5,
        "width_mm": 114.5,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "SINOTRUK / Shacman Heavy",
        "vehicles": "SINOTRUK Heavy Export Tractor & Dump Truck",
        "axle": "SINOTRUK Export",
        "markets": ["Middle East", "South America", "Southeast Asia"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "WG5449050": {
        "wva": "WG5449050",
        "gys": "GYD-WG544",
        "size": "250.0 × 100.3 × 30.0",
        "length_mm": 250.0,
        "width_mm": 100.3,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "SINOTRUK / Heavy Dump",
        "vehicles": "SINOTRUK HOWO Mining & Long-Haul Tractors",
        "axle": "SINOTRUK Heavy",
        "markets": ["Southeast Asia", "South America", "Middle East"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "HL3501GA": {
        "wva": "HL3501GA-06-01",
        "gys": "GYD-HL3501",
        "size": "210.5 × 99.5 × 29.0",
        "length_mm": 210.5,
        "width_mm": 99.5,
        "thickness_mm": 29.0,
        "holes": 0,
        "caliper": "Hande Disc Axle",
        "vehicles": "Shacman / Hande Disc Axle Light-Heavy Duty",
        "axle": "Hande Disc",
        "markets": ["Southeast Asia", "Middle East"],
        "formula": "GY-CV360 (Low-Noise Fleet Formula)",
    },
    "LXGC锣响": {
        "wva": "LXGC Trailer Disc",
        "gys": "GYD-LXGC",
        "size": "210.5 × 109.5 × 30.0",
        "length_mm": 210.5,
        "width_mm": 109.5,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "LXGC Disc Trailer",
        "vehicles": "Lightweight High-Strength Semi-Trailers",
        "axle": "LXGC Disc Axle",
        "markets": ["Southeast Asia", "South America"],
        "formula": "N91 (Mineral-Carbon Eco)",
    },
    "LZ19.5": {
        "wva": "LZ 19.5K",
        "gys": "GYD-LZ195",
        "size": "210.5 × 92.5 × 30.0",
        "length_mm": 210.5,
        "width_mm": 92.5,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "LZ 19.5K Trailer",
        "vehicles": "19.5\" Heavy Logistics Trailer Axles",
        "axle": "LZ 19.5K",
        "markets": ["Southeast Asia", "Middle East"],
        "formula": "GY-CV360 (Low-Noise Fleet Formula)",
    },
    "合力20寸": {
        "wva": "Heli 20\"",
        "gys": "GYD-HELI20",
        "size": "220.0 × 105.0 × 30.0",
        "length_mm": 220.0,
        "width_mm": 105.0,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "20\" Heavy Port & Forklift",
        "vehicles": "Heavy Container Handler & Port Terminal Trucks",
        "axle": "20\" Heavy Industrial",
        "markets": ["Southeast Asia", "Middle East", "North America"],
        "formula": "GY-HD400 (High-Temp Carbon Ceramic)",
    },
    "宇通01": {
        "wva": "Yutong-01 (WVA 29087 Alt)",
        "gys": "GYD-YT01",
        "size": "247.6 × 109.7 × 30.0",
        "length_mm": 247.6,
        "width_mm": 109.7,
        "thickness_mm": 30.0,
        "holes": 0,
        "caliper": "Yutong 22.5\" Front/Rear",
        "vehicles": "Yutong ZK6122 / Intercity & Export Transit Buses",
        "axle": "Yutong Coach 22.5\"",
        "markets": ["Middle East", "South America", "Southeast Asia"],
        "formula": "GY-CV360 (Low-Noise Fleet Formula)",
    },
    "宇通02": {
        "wva": "Yutong-02",
        "gys": "GYD-YT02",
        "size": "210.5 × 99.6 × 29.0",
        "length_mm": 210.5,
        "width_mm": 99.6,
        "thickness_mm": 29.0,
        "holes": 0,
        "caliper": "Yutong 19.5\" Transit",
        "vehicles": "Yutong ZK6907 / ZK6107 City Transit Fleet",
        "axle": "Yutong 19.5\"",
        "markets": ["Middle East", "Southeast Asia", "South America"],
        "formula": "GY-CV360 (Low-Noise Fleet Formula)",
    },
}

# Verified Drum specs from kooying catalog.pdf & 冠隆产品册.pdf
DRUM_RULES: list[tuple[str, dict[str, Any]]] = [
    ("19369", {"wva": "WVA 19369 / 4707Q", "gys": "GYS-4707Q", "size": "215 × 178 × 19.0/12.5", "length_mm": 215.0, "width_mm": 178.0, "thickness_mm": 19.0, "holes": 8, "axle": "Meritor Q-Plus 16.5×7\"", "vehicles": "North American Class 8 Trucks & Freightliner/Kenworth Trailers", "markets": ["North America", "South America"], "formula": "GY-HD400"}),
    ("19370", {"wva": "WVA 19370 / 4515Q", "gys": "GYS-4515Q", "size": "212 × 178 × 18.8/12.0", "length_mm": 212.0, "width_mm": 178.0, "thickness_mm": 18.8, "holes": 8, "axle": "Meritor Q-Series 16.5×7\"", "vehicles": "North American Semi-Trailers & Heavy Tractors", "markets": ["North America", "South America"], "formula": "GY-HD400"}),
    ("19553", {"wva": "WVA 19553", "gys": "GYS-19553", "size": "203.2 × 200 × 22.0/14.0", "length_mm": 203.2, "width_mm": 200.0, "thickness_mm": 22.0, "holes": 12, "axle": "Meritor / Heavy Drive 8\"", "vehicles": "North & South American Heavy-Duty 8\" Trailer Axles", "markets": ["North America", "South America", "Middle East"], "formula": "GY-HD400"}),
    ("19932斯堪尼亚", {"wva": "WVA 19932", "gys": "GYS-19932", "size": "260 × 204 × 19.0", "length_mm": 260.0, "width_mm": 204.0, "thickness_mm": 19.0, "holes": 16, "axle": "SCANIA P/G/R Heavy Drum", "vehicles": "SCANIA 113 / 114 / P360 / R420 Heavy Trucks & Buses", "markets": ["South America", "Middle East", "Europe"], "formula": "GY-HD400"}),
    ("307AF-1", {"wva": "WVA 307AF-1", "gys": "GYS-143", "size": "323 × 85 × 10.0", "length_mm": 323.0, "width_mm": 85.0, "thickness_mm": 10.0, "holes": 10, "axle": "Existing Models 307AF", "vehicles": "Commercial Bus & Light-Medium Export Drum", "markets": ["Southeast Asia", "South America"], "formula": "GY-CV360"}),
    ("307AF-2", {"wva": "WVA 307AF-2", "gys": "GYS-144", "size": "323 × 85 × 10.0", "length_mm": 323.0, "width_mm": 85.0, "thickness_mm": 10.0, "holes": 12, "axle": "Existing Models 307AF", "vehicles": "Commercial Bus & Export Drum (12-Hole Spec)", "markets": ["Southeast Asia", "South America"], "formula": "GY-CV360"}),
    ("307AF", {"wva": "WVA 307AF", "gys": "GYS-143S", "size": "323 × 85 × 10.0", "length_mm": 323.0, "width_mm": 85.0, "thickness_mm": 10.0, "holes": 10, "axle": "Existing Models 307AF", "vehicles": "Export Light-Medium Commercial Vehicles", "markets": ["Southeast Asia", "South America", "Middle East"], "formula": "GY-CV360"}),
    ("311AF", {"wva": "WVA 311AF", "gys": "GYS-145", "size": "341 × 100 × 12.0", "length_mm": 341.0, "width_mm": 100.0, "thickness_mm": 12.0, "holes": 12, "axle": "Existing Models 311AF", "vehicles": "Medium Commercial Truck & Transit Bus", "markets": ["Southeast Asia", "South America", "Middle East"], "formula": "GY-CV360"}),
    ("37-1", {"wva": "WVA 19094 / BC/37/1", "gys": "GYS-018", "size": "220 × 200 × 17.5/10.5", "length_mm": 220.0, "width_mm": 200.0, "thickness_mm": 17.5, "holes": 10, "axle": "BPW BC/37/1 Trailer Axle", "vehicles": "BPW German Trailer Axles 12T–14T Series", "markets": ["Europe", "Middle East", "South America"], "formula": "GY-HD400"}),
    ("西德拖架BC36-1", {"wva": "WVA 19032 / BC/36/1", "gys": "GYS-007", "size": "220 × 180 × 17.5/10.5", "length_mm": 220.0, "width_mm": 180.0, "thickness_mm": 17.5, "holes": 10, "axle": "BPW BC/36/1 (180mm)", "vehicles": "BPW European Semi-Trailer Axle 10–12T", "markets": ["Europe", "Middle East", "South America"], "formula": "GY-HD400"}),
    ("西德拖架BC37-1-2", {"wva": "WVA 19094 / BC/37/1-2", "gys": "GYS-018B", "size": "220 × 200 × 17.5/10.5", "length_mm": 220.0, "width_mm": 200.0, "thickness_mm": 17.5, "holes": 10, "axle": "BPW BC/37/1 Dual", "vehicles": "BPW Heavy Trailer Axle 12T–14T", "markets": ["Europe", "Middle East", "South America"], "formula": "GY-HD400"}),
    ("西德拖架BC37-1", {"wva": "WVA 19094 / BC/37/1", "gys": "GYS-018", "size": "220 × 200 × 17.5/10.5", "length_mm": 220.0, "width_mm": 200.0, "thickness_mm": 17.5, "holes": 10, "axle": "BPW BC/37/1 (200mm)", "vehicles": "BPW German Semi-Trailer Axle 12T–14T", "markets": ["Europe", "Middle East", "South America"], "formula": "GY-HD400"}),
    ("西德拖架BC38-1", {"wva": "BPW BC/38/1", "gys": "GYS-BC38", "size": "220 × 200 × 18.0/11.0", "length_mm": 220.0, "width_mm": 200.0, "thickness_mm": 18.0, "holes": 10, "axle": "BPW BC/38/1", "vehicles": "BPW 14T–16T Heavy European Trailer", "markets": ["Europe", "Middle East", "South America"], "formula": "GY-HD400"}),
    ("西德拖架BC39-1", {"wva": "BPW BC/39/1 (11.5T)", "gys": "GYS-BC39", "size": "214 × 200 × 17.5/11.0", "length_mm": 214.0, "width_mm": 200.0, "thickness_mm": 17.5, "holes": 10, "axle": "BPW 11.5T Heavy Trailer", "vehicles": "BPW 11.5T Trailer Axle (Middle East / South America Spec)", "markets": ["Middle East", "South America", "Europe"], "formula": "GY-HD400"}),
    ("美驰桥A", {"wva": "Meritor A", "gys": "GYS-MR-A", "size": "212 × 178 × 14.5/18.5", "length_mm": 212.0, "width_mm": 178.0, "thickness_mm": 18.5, "holes": 14, "axle": "Meritor Axle A", "vehicles": "Meritor Heavy Drive Axle / SINOTRUK Huanghe Prince", "markets": ["North America", "South America", "Middle East"], "formula": "GY-HD400"}),
    ("美驰桥B", {"wva": "Meritor B", "gys": "GYS-MR-B", "size": "212 × 178 × 10.5/18.5", "length_mm": 212.0, "width_mm": 178.0, "thickness_mm": 18.5, "holes": 14, "axle": "Meritor Axle B", "vehicles": "Meritor Heavy Drive Axle / SINOTRUK Huanghe Prince", "markets": ["North America", "South America", "Middle East"], "formula": "GY-HD400"}),
    ("美国托架12TA", {"wva": "US Axle 12T-A", "gys": "GYS-US12A", "size": "210 × 178 × 18.0/12.0", "length_mm": 210.0, "width_mm": 178.0, "thickness_mm": 18.0, "holes": 8, "axle": "American Trailer 12T-A", "vehicles": "American 12T Semi-Trailer Axle (16.5×7\")", "markets": ["North America", "South America"], "formula": "GY-HD400"}),
    ("美国托架12TB", {"wva": "US Axle 12T-B", "gys": "GYS-US12B", "size": "210 × 178 × 18.0/12.0", "length_mm": 210.0, "width_mm": 178.0, "thickness_mm": 18.0, "holes": 8, "axle": "American Trailer 12T-B", "vehicles": "American 12T Semi-Trailer Axle (16.5×7\")", "markets": ["North America", "South America"], "formula": "GY-HD400"}),
    ("富华桥11.5TA", {"wva": "Fuwa 11.5T-A", "gys": "GYS-FW115A", "size": "165 × 200 × 12.5/16.8", "length_mm": 165.0, "width_mm": 200.0, "thickness_mm": 16.8, "holes": 8, "axle": "Fuwa 11.5T Trailer", "vehicles": "Fuwa 11.5T Heavy Trailer Axle", "markets": ["Southeast Asia", "Middle East", "South America"], "formula": "GY-HD400"}),
    ("富华桥11.5TB", {"wva": "Fuwa 11.5T-B", "gys": "GYS-FW115B", "size": "214 × 200 × 11.0/16.8", "length_mm": 214.0, "width_mm": 200.0, "thickness_mm": 16.8, "holes": 10, "axle": "Fuwa 11.5T Trailer", "vehicles": "Fuwa 11.5T Heavy Trailer Axle", "markets": ["Southeast Asia", "Middle East", "South America"], "formula": "GY-HD400"}),
    ("富华桥12TA", {"wva": "Fuwa 12T-A", "gys": "GYS-FW12A", "size": "210 × 180 × 16.0/12.0", "length_mm": 210.0, "width_mm": 180.0, "thickness_mm": 16.0, "holes": 8, "axle": "Fuwa 12T Trailer", "vehicles": "Fuwa 12T Semi-Trailer Axle", "markets": ["Southeast Asia", "Middle East", "South America"], "formula": "GY-HD400"}),
    ("富华桥12TB", {"wva": "Fuwa 12T-B", "gys": "GYS-FW12B", "size": "210 × 180 × 16.0/12.0", "length_mm": 210.0, "width_mm": 180.0, "thickness_mm": 16.0, "holes": 8, "axle": "Fuwa 12T Trailer", "vehicles": "Fuwa 12T Semi-Trailer Axle", "markets": ["Southeast Asia", "Middle East", "South America"], "formula": "GY-HD400"}),
    ("富华桥16TA-302", {"wva": "Fuwa 16T-302", "gys": "GYS-FW16A3", "size": "215 × 200 × 18.0/13.0", "length_mm": 215.0, "width_mm": 200.0, "thickness_mm": 18.0, "holes": 10, "axle": "Fuwa 16T Heavy", "vehicles": "Fuwa 16T Heavy Logging & Mining Trailers", "markets": ["Southeast Asia", "Middle East", "South America"], "formula": "GY-HD400"}),
    ("富华桥16TA", {"wva": "Fuwa 16T-A", "gys": "GYS-FW16A", "size": "215 × 200 × 18.0/12.0", "length_mm": 215.0, "width_mm": 200.0, "thickness_mm": 18.0, "holes": 10, "axle": "Fuwa 16T Trailer", "vehicles": "Fuwa 16T Semi-Trailer Axle", "markets": ["Southeast Asia", "Middle East", "South America"], "formula": "GY-HD400"}),
    ("富华桥16TB", {"wva": "Fuwa 16T-B", "gys": "GYS-FW16B", "size": "215 × 200 × 18.0/12.0", "length_mm": 215.0, "width_mm": 200.0, "thickness_mm": 18.0, "holes": 10, "axle": "Fuwa 16T Trailer", "vehicles": "Fuwa 16T Semi-Trailer Axle", "markets": ["Southeast Asia", "Middle East", "South America"], "formula": "GY-HD400"}),
    ("富华桥200宽", {"wva": "Fuwa 200mm Wide", "gys": "GYS-FW200", "size": "215 × 200 × 17.5", "length_mm": 215.0, "width_mm": 200.0, "thickness_mm": 17.5, "holes": 10, "axle": "Fuwa 200mm Wide", "vehicles": "Fuwa 200mm Wide Trailer Axle", "markets": ["Southeast Asia", "Middle East"], "formula": "GY-HD400"}),
    ("日野前", {"wva": "HINO ZY F", "gys": "GYS-119", "size": "230 × 126 × 15.5", "length_mm": 230.0, "width_mm": 126.0, "thickness_mm": 15.5, "holes": 8, "axle": "HINO 700 / ZY Front", "vehicles": "HINO 700 / Profia / ZY Front Axle", "markets": ["Southeast Asia", "Middle East", "South America"], "formula": "GY-CV360"}),
    ("日野后", {"wva": "HINO ZY R", "gys": "GYS-120", "size": "230 × 202 × 15.5", "length_mm": 230.0, "width_mm": 202.0, "thickness_mm": 15.5, "holes": 12, "axle": "HINO 700 / ZY Rear", "vehicles": "HINO 700 / Profia / ZY Rear Axle", "markets": ["Southeast Asia", "Middle East", "South America"], "formula": "GY-HD400"}),
    ("CVR146五十铃前", {"wva": "ISUZU CVR146 F", "gys": "GYS-ISZ-F", "size": "205 × 130 × 15.0", "length_mm": 205.0, "width_mm": 130.0, "thickness_mm": 15.0, "holes": 8, "axle": "ISUZU CVR/CXZ Front", "vehicles": "ISUZU CVR146 / Giga / FVR Front Axle", "markets": ["Southeast Asia", "South America"], "formula": "GY-CV360"}),
    ("CVR146五十铃后", {"wva": "ISUZU CVR146 R", "gys": "GYS-ISZ-R", "size": "215 × 220 × 15.0", "length_mm": 215.0, "width_mm": 220.0, "thickness_mm": 15.0, "holes": 14, "axle": "ISUZU CVR/CXZ Rear", "vehicles": "ISUZU CVR146 / Giga / CXZ Rear Axle", "markets": ["Southeast Asia", "South America"], "formula": "GY-HD400"}),
    ("五十铃前", {"wva": "ISUZU Heavy F", "gys": "GYS-ISZ-HF", "size": "210 × 160 × 16.0", "length_mm": 210.0, "width_mm": 160.0, "thickness_mm": 16.0, "holes": 10, "axle": "ISUZU Giga Front", "vehicles": "ISUZU Heavy Truck Front Axle", "markets": ["Southeast Asia", "Middle East"], "formula": "GY-CV360"}),
    ("五十铃后", {"wva": "ISUZU Heavy R", "gys": "GYS-ISZ-HR", "size": "215 × 220 × 14.5", "length_mm": 215.0, "width_mm": 220.0, "thickness_mm": 14.5, "holes": 17, "axle": "ISUZU Giga Rear", "vehicles": "ISUZU Heavy Truck Rear Axle", "markets": ["Southeast Asia", "Middle East"], "formula": "GY-HD400"}),
    ("现代前A", {"wva": "HYUNDAI BUS F1", "gys": "GYS-133", "size": "205 × 155 × 18.0/10.0", "length_mm": 205.0, "width_mm": 155.0, "thickness_mm": 18.0, "holes": 10, "axle": "HYUNDAI Front A", "vehicles": "HYUNDAI Universe / Aero City / HD1000 Front", "markets": ["Southeast Asia", "Middle East", "South America"], "formula": "GY-CV360"}),
    ("现代前B", {"wva": "HYUNDAI BUS F2", "gys": "GYS-134", "size": "202 × 155 × 18.0/16.0", "length_mm": 202.0, "width_mm": 155.0, "thickness_mm": 18.0, "holes": 10, "axle": "HYUNDAI Front B", "vehicles": "HYUNDAI Universe / Heavy Front", "markets": ["Southeast Asia", "Middle East", "South America"], "formula": "GY-CV360"}),
    ("现代后A", {"wva": "HYUNDAI BUS R1", "gys": "GYS-135", "size": "200 × 220 × 18.0/16.0", "length_mm": 200.0, "width_mm": 220.0, "thickness_mm": 18.0, "holes": 13, "axle": "HYUNDAI Rear A", "vehicles": "HYUNDAI Trago / Xcient / Bus Rear Axle", "markets": ["Southeast Asia", "Middle East", "South America"], "formula": "GY-HD400"}),
    ("现代后B", {"wva": "HYUNDAI BUS R2", "gys": "GYS-136", "size": "195 × 220 × 18.0/10.5", "length_mm": 195.0, "width_mm": 220.0, "thickness_mm": 18.0, "holes": 13, "axle": "HYUNDAI Rear B", "vehicles": "HYUNDAI Trago / Xcient / Bus Rear Axle", "markets": ["Southeast Asia", "Middle East", "South America"], "formula": "GY-HD400"}),
    ("现代HYUNDAI", {"wva": "HYUNDAI R1/R2", "gys": "GYS-141", "size": "200 × 220 × 18.0/15.0", "length_mm": 200.0, "width_mm": 220.0, "thickness_mm": 18.0, "holes": 16, "axle": "HYUNDAI Heavy Rear", "vehicles": "HYUNDAI Heavy Dump & Tractor Axle", "markets": ["Southeast Asia", "Middle East"], "formula": "GY-HD400"}),
    ("沃尔沃后", {"wva": "WVA 19071 / VI88", "gys": "GYS-015", "size": "195 × 198 × 18.0/13.0", "length_mm": 195.0, "width_mm": 198.0, "thickness_mm": 18.0, "holes": 14, "axle": "VOLVO Rear Drum", "vehicles": "VOLVO FL / FM / F12 Rear Drum Axle", "markets": ["Europe", "South America", "Middle East"], "formula": "GY-HD400"}),
    ("斯太尔前A", {"wva": "WVA 19245 / SY/23/1", "gys": "GYS-023", "size": "200 × 160 × 14.5", "length_mm": 200.0, "width_mm": 160.0, "thickness_mm": 14.5, "holes": 8, "axle": "STEYR Front A", "vehicles": "STEYR / SINOTRUK / Shacman Front Drum Axle", "markets": ["Southeast Asia", "Middle East", "South America"], "formula": "GY-CV360"}),
    ("斯太尔前B", {"wva": "WVA 19245 / SY/23/2", "gys": "GYS-123", "size": "200 × 160 × 14.5", "length_mm": 200.0, "width_mm": 160.0, "thickness_mm": 14.5, "holes": 8, "axle": "STEYR Front B", "vehicles": "STEYR / SINOTRUK / Shacman Front Drum Axle", "markets": ["Southeast Asia", "Middle East", "South America"], "formula": "GY-CV360"}),
    ("斯太尔后", {"wva": "WVA 19246 / SY/24/1", "gys": "GYS-024", "size": "201 × 185 × 14.5", "length_mm": 201.0, "width_mm": 185.0, "thickness_mm": 14.5, "holes": 8, "axle": "STEYR Rear", "vehicles": "STEYR / SINOTRUK Heavy Rear Axle", "markets": ["Southeast Asia", "Middle East", "South America"], "formula": "GY-HD400"}),
    ("新豪沃A7", {"wva": "HOWO A7 / WVA 19247", "gys": "GYS-125", "size": "204 × 220 × 15.0", "length_mm": 204.0, "width_mm": 220.0, "thickness_mm": 15.0, "holes": 14, "axle": "SINOTRUK HOWO A7", "vehicles": "SINOTRUK HOWO A7 / T5G Heavy Tractor", "markets": ["Southeast Asia", "South America", "Middle East"], "formula": "GY-HD400"}),
    ("新重汽后", {"wva": "SINOTRUK Heavy R", "gys": "GYS-126", "size": "204 × 220 × 15.0", "length_mm": 204.0, "width_mm": 220.0, "thickness_mm": 15.0, "holes": 14, "axle": "SINOTRUK AC16/MCY13", "vehicles": "SINOTRUK HOWO / Shacman Heavy Dump Truck", "markets": ["Southeast Asia", "South America", "Middle East"], "formula": "GY-HD400"}),
    ("重汽汕德卡后", {"wva": "SITRAK C7H Rear", "gys": "GYS-MCY13", "size": "208 × 220 × 16.5", "length_mm": 208.0, "width_mm": 220.0, "thickness_mm": 16.5, "holes": 16, "axle": "MAN MCY13 Axle", "vehicles": "SINOTRUK SITRAK C7H / C9H Heavy Tractor", "markets": ["Middle East", "South America", "Southeast Asia"], "formula": "GY-HD400"}),
    ("北方奔驰1622前", {"wva": "BEIBEN 1622 F", "gys": "GYS-001", "size": "198 × 140 × 15.0", "length_mm": 198.0, "width_mm": 140.0, "thickness_mm": 15.0, "holes": 8, "axle": "Mercedes / Beiben Front", "vehicles": "Beiben NG80 / Mercedes-Benz 1622 Front", "markets": ["Middle East", "South America"], "formula": "GY-HD400"}),
    ("北方奔驰1622后", {"wva": "BEIBEN 1622 R", "gys": "GYS-BB1622R", "size": "205 × 170 × 15.0", "length_mm": 205.0, "width_mm": 170.0, "thickness_mm": 15.0, "holes": 8, "axle": "Mercedes / Beiben Rear", "vehicles": "Beiben NG80 / Mercedes-Benz 1622 Rear", "markets": ["Middle East", "South America"], "formula": "GY-HD400"}),
    ("北方奔驰180宽", {"wva": "BEIBEN 180mm Wide", "gys": "GYS-BB180", "size": "206 × 180 × 16.0", "length_mm": 206.0, "width_mm": 180.0, "thickness_mm": 16.0, "holes": 10, "axle": "Beiben Heavy 180mm", "vehicles": "Beiben Heavy Mining & Desert Haulage", "markets": ["Middle East", "South America"], "formula": "GY-HD400"}),
    ("北方奔驰", {"wva": "BEIBEN Heavy", "gys": "GYS-BB01", "size": "206 × 180 × 15.5", "length_mm": 206.0, "width_mm": 180.0, "thickness_mm": 15.5, "holes": 10, "axle": "Beiben / Benz Heavy", "vehicles": "Beiben / Mercedes Heavy Export Fleet", "markets": ["Middle East", "South America"], "formula": "GY-HD400"}),
    ("EQ153前", {"wva": "EQ153 F", "gys": "GYS-127", "size": "200 × 130 × 14.5", "length_mm": 200.0, "width_mm": 130.0, "thickness_mm": 14.5, "holes": 6, "axle": "Dongfeng EQ153 Front", "vehicles": "Dongfeng EQ153 / Yutong / KingLong Medium", "markets": ["Southeast Asia", "South America"], "formula": "GY-CV360"}),
    ("EQ153后", {"wva": "EQ153 R", "gys": "GYS-128", "size": "213 × 180 × 17.0", "length_mm": 213.0, "width_mm": 180.0, "thickness_mm": 17.0, "holes": 9, "axle": "Dongfeng EQ153 Rear", "vehicles": "Dongfeng EQ153 Rear Drum Axle", "markets": ["Southeast Asia", "South America"], "formula": "GY-CV360"}),
    ("新153后-N91配方", {"wva": "NEW EQ153 R (N91)", "gys": "GYS-130-N91", "size": "208 × 220 × 15.0", "length_mm": 208.0, "width_mm": 220.0, "thickness_mm": 15.0, "holes": 15, "axle": "New EQ153 / Yutong Rear", "vehicles": "New EQ153, Yutong, KingLong, Higer (N91 Mineral-Carbon Formula)", "markets": ["Southeast Asia", "Middle East", "South America"], "formula": "N91 (Mineral-Carbon Eco)"}),
    ("新153前", {"wva": "NEW EQ153 F", "gys": "GYS-129", "size": "208 × 150 × 15.0", "length_mm": 208.0, "width_mm": 150.0, "thickness_mm": 15.0, "holes": 10, "axle": "New EQ153 Front", "vehicles": "New EQ153, Yutong, KingLong, Higer", "markets": ["Southeast Asia", "Middle East"], "formula": "GY-CV360"}),
    ("新153后", {"wva": "NEW EQ153 R", "gys": "GYS-130", "size": "208 × 220 × 15.0", "length_mm": 208.0, "width_mm": 220.0, "thickness_mm": 15.0, "holes": 15, "axle": "New EQ153 Rear", "vehicles": "New EQ153, Yutong, KingLong, Higer", "markets": ["Southeast Asia", "Middle East"], "formula": "GY-HD400"}),
    ("奥威后A", {"wva": "JIEFANG Aowei R-A", "gys": "GYS-FAW-AWA", "size": "165 × 220 × 16.5/18.0", "length_mm": 165.0, "width_mm": 220.0, "thickness_mm": 18.0, "holes": 12, "axle": "FAW Aowei 457/485", "vehicles": "FAW JIEFANG Aowei Rear Axle (ABS)", "markets": ["Southeast Asia", "South America"], "formula": "GY-HD400"}),
    ("奥威后B", {"wva": "JIEFANG Aowei R-B", "gys": "GYS-FAW-AWB", "size": "228 × 220 × 11.0/18.0", "length_mm": 228.0, "width_mm": 220.0, "thickness_mm": 18.0, "holes": 16, "axle": "FAW Aowei 457/485", "vehicles": "FAW JIEFANG Aowei Rear Axle", "markets": ["Southeast Asia", "South America"], "formula": "GY-HD400"}),
    ("新奥威前A", {"wva": "NEW Aowei F-A", "gys": "GYS-FAW-NAFA", "size": "208 × 150 × 16.0/20.0", "length_mm": 208.0, "width_mm": 150.0, "thickness_mm": 20.0, "holes": 8, "axle": "FAW New Aowei Front", "vehicles": "FAW JIEFANG New Aowei Front Axle", "markets": ["Southeast Asia", "Middle East"], "formula": "GY-CV360"}),
    ("新奥威前B", {"wva": "NEW Aowei F-B", "gys": "GYS-FAW-NAFB", "size": "202 × 150 × 9.0/20.0", "length_mm": 202.0, "width_mm": 150.0, "thickness_mm": 20.0, "holes": 8, "axle": "FAW New Aowei Front", "vehicles": "FAW JIEFANG New Aowei Front Axle", "markets": ["Southeast Asia", "Middle East"], "formula": "GY-CV360"}),
    ("新奥威后A", {"wva": "NEW Aowei R-A", "gys": "GYS-FAW-NARA", "size": "208 × 200 × 16.0/20.0", "length_mm": 208.0, "width_mm": 200.0, "thickness_mm": 20.0, "holes": 16, "axle": "FAW New Aowei Rear", "vehicles": "FAW JIEFANG New Aowei Rear Axle (ABS)", "markets": ["Southeast Asia", "South America"], "formula": "GY-HD400"}),
    ("新奥威后B", {"wva": "NEW Aowei R-B", "gys": "GYS-FAW-NARB", "size": "202 × 200 × 9.0/20.0", "length_mm": 202.0, "width_mm": 200.0, "thickness_mm": 20.0, "holes": 16, "axle": "FAW New Aowei Rear", "vehicles": "FAW JIEFANG New Aowei Rear Axle", "markets": ["Southeast Asia", "South America"], "formula": "GY-HD400"}),
    ("解放途威后A", {"wva": "JIEFANG Tuwei R-A", "gys": "GYS-FAW-TWA", "size": "197 × 200 × 17.0/20.0", "length_mm": 197.0, "width_mm": 200.0, "thickness_mm": 20.0, "holes": 16, "axle": "FAW Tuwei / Kooying Star", "vehicles": "FAW JIEFANG Tuwei / JH6 Heavy Rear", "markets": ["Southeast Asia", "Middle East", "South America"], "formula": "GY-HD400 (Kooying Star)"}),
    ("解放途威后B", {"wva": "JIEFANG Tuwei R-B", "gys": "GYS-FAW-TWB", "size": "201 × 200 × 11.5/20.0", "length_mm": 201.0, "width_mm": 200.0, "thickness_mm": 20.0, "holes": 16, "axle": "FAW Tuwei / Kooying Star", "vehicles": "FAW JIEFANG Tuwei / JH6 Heavy Rear", "markets": ["Southeast Asia", "Middle East", "South America"], "formula": "GY-HD400 (Kooying Star)"}),
    ("解放J6后A", {"wva": "JIEFANG J6 R-A", "gys": "GYS-FAW-J6A", "size": "200 × 200 × 17.0/20.0", "length_mm": 200.0, "width_mm": 200.0, "thickness_mm": 20.0, "holes": 16, "axle": "FAW J6 457 Axle", "vehicles": "FAW JIEFANG J6P / J6L Rear Axle", "markets": ["Southeast Asia", "South America"], "formula": "GY-HD400"}),
    ("解放J6后B", {"wva": "JIEFANG J6 R-B", "gys": "GYS-FAW-J6B", "size": "200 × 200 × 12.0/20.0", "length_mm": 200.0, "width_mm": 200.0, "thickness_mm": 20.0, "holes": 16, "axle": "FAW J6 457 Axle", "vehicles": "FAW JIEFANG J6P / J6L Rear Axle", "markets": ["Southeast Asia", "South America"], "formula": "GY-HD400"}),
    ("新J6前A", {"wva": "NEW J6 Front-A", "gys": "GYS-FAW-NJ6FA", "size": "209 × 150 × 16.0/18.0", "length_mm": 209.0, "width_mm": 150.0, "thickness_mm": 18.0, "holes": 8, "axle": "FAW New J6 Front", "vehicles": "FAW JIEFANG New J6 Front Axle", "markets": ["Southeast Asia", "South America"], "formula": "GY-CV360"}),
    ("新J6前B", {"wva": "NEW J6 Front-B", "gys": "GYS-FAW-NJ6FB", "size": "202 × 150 × 10.0/18.0", "length_mm": 202.0, "width_mm": 150.0, "thickness_mm": 18.0, "holes": 8, "axle": "FAW New J6 Front", "vehicles": "FAW JIEFANG New J6 Front Axle", "markets": ["Southeast Asia", "South America"], "formula": "GY-CV360"}),
    ("解放J5前", {"wva": "JIEFANG J5 Front", "gys": "GYS-FAW-J5F", "size": "205 × 135 × 15.0", "length_mm": 205.0, "width_mm": 135.0, "thickness_mm": 15.0, "holes": 8, "axle": "FAW J5 Front", "vehicles": "FAW JIEFANG J5M/J5P Front Axle", "markets": ["Southeast Asia", "South America"], "formula": "GY-CV360"}),
    ("小悍威前", {"wva": "JIEFANG Hanwei Front", "gys": "GYS-FAW-HWF", "size": "218 × 135 × 16.0/19.5", "length_mm": 218.0, "width_mm": 135.0, "thickness_mm": 19.5, "holes": 8, "axle": "FAW Hanwei Front", "vehicles": "FAW JIEFANG Hanwei Front Axle", "markets": ["Southeast Asia", "South America"], "formula": "GY-CV360"}),
    ("德龙曼前", {"wva": "SHACMAN Delong MAN F", "gys": "GYS-DL-MF", "size": "195 × 180 × 12.5/16.0", "length_mm": 195.0, "width_mm": 180.0, "thickness_mm": 16.0, "holes": 8, "axle": "Hande MAN Front", "vehicles": "SHACMAN Delong X3000/F3000 MAN Front Axle", "markets": ["Middle East", "South America", "Southeast Asia"], "formula": "GY-HD400"}),
    ("德龙曼后", {"wva": "SHACMAN Delong MAN R", "gys": "GYS-DL-MR", "size": "214 × 220 × 14.5/18.5", "length_mm": 214.0, "width_mm": 220.0, "thickness_mm": 18.5, "holes": 16, "axle": "Hande MAN 16T Rear", "vehicles": "SHACMAN Delong X3000/M3000 Hande MAN Rear Axle", "markets": ["Middle East", "South America", "Southeast Asia"], "formula": "GY-HD400"}),
    ("德龙新M3000", {"wva": "SHACMAN M3000", "gys": "GYS-DL-M3K", "size": "214 × 220 × 14.5/18.5", "length_mm": 214.0, "width_mm": 220.0, "thickness_mm": 18.5, "holes": 16, "axle": "Hande M3000 Axle", "vehicles": "SHACMAN New M3000 Tractor & Dump Truck", "markets": ["Middle East", "Southeast Asia", "South America"], "formula": "GY-HD400"}),
    ("天龙旗舰后", {"wva": "Dongfeng KX Flagship R", "gys": "GYS-DF-KXR", "size": "215 × 220 × 16.0/19.0", "length_mm": 215.0, "width_mm": 220.0, "thickness_mm": 19.0, "holes": 16, "axle": "Dana / Dongfeng 465", "vehicles": "Dongfeng KX / Tianlong Flagship Long-Haul", "markets": ["Southeast Asia", "Middle East"], "formula": "GY-HD400"}),
    ("克拉斯", {"wva": "KrAZ Mining Heavy", "gys": "GYS-KRAZ", "size": "225 × 140 × 18.0", "length_mm": 225.0, "width_mm": 140.0, "thickness_mm": 18.0, "holes": 10, "axle": "KrAZ Off-Road Mining", "vehicles": "KrAZ Heavy Timber, Mining & Oilfield Trucks", "markets": ["South America", "Middle East"], "formula": "GY-HD400 (Extreme Mining)"}),
    ("别拉斯", {"wva": "BELAZ Mining Heavy", "gys": "GYS-BELAZ", "size": "245 × 180 × 20.0", "length_mm": 245.0, "width_mm": 180.0, "thickness_mm": 20.0, "holes": 14, "axle": "BELAZ Dump Mining", "vehicles": "BELAZ Heavy Quarry & Open-Pit Mining Dumpers", "markets": ["South America", "Middle East", "North America"], "formula": "GY-HD400 (Extreme Mining)"}),
    ("联合重卡", {"wva": "C&C Heavy Truck", "gys": "GYS-CC-R", "size": "190 × 220 × 12.0/16.0", "length_mm": 190.0, "width_mm": 220.0, "thickness_mm": 16.0, "holes": 14, "axle": "C&C 460 Rear", "vehicles": "C&C Heavy Duty LNG & Diesel Tractors", "markets": ["Southeast Asia", "South America"], "formula": "GY-HD400"}),
    ("广州托架", {"wva": "Guangzhou Trailer Axle", "gys": "GYS-GZ-TR", "size": "212 × 180 × 16.0", "length_mm": 212.0, "width_mm": 180.0, "thickness_mm": 16.0, "holes": 8, "axle": "Port & Logistics Trailer", "vehicles": "Heavy Container Semi-Trailers (Port Spec)", "markets": ["Southeast Asia", "Middle East"], "formula": "GY-CV360"}),
    ("CA485", {"wva": "FAW CA485 Rear", "gys": "GYS-485", "size": "204 × 200 × 16.5", "length_mm": 204.0, "width_mm": 200.0, "thickness_mm": 16.5, "holes": 14, "axle": "FAW 485 Drive Axle", "vehicles": "FAW Heavy Dump & Construction Trucks", "markets": ["Southeast Asia", "Middle East"], "formula": "GY-HD400"}),
    ("HTC-4F", {"wva": "HTC-4F", "gys": "GYS-HTC4F", "size": "206 × 180 × 15.5", "length_mm": 206.0, "width_mm": 180.0, "thickness_mm": 15.5, "holes": 10, "axle": "Export Commercial Axle", "vehicles": "Heavy Commercial Export Fleet Spec", "markets": ["Middle East", "South America"], "formula": "GY-HD400"}),
    ("T850", {"wva": "T850 Heavy", "gys": "GYS-T850", "size": "210 × 200 × 16.5", "length_mm": 210.0, "width_mm": 200.0, "thickness_mm": 16.5, "holes": 12, "axle": "Heavy Mining T850", "vehicles": "Heavy Export Dump & Engineering Chassis", "markets": ["Middle East", "South America"], "formula": "GY-HD400"}),
    ("福田奥铃", {"wva": "FOTON Aumark/Ollin", "gys": "GYS-FT-OL", "size": "148 × 100 × 13.0", "length_mm": 148.0, "width_mm": 100.0, "thickness_mm": 13.0, "holes": 6, "axle": "Foton Light-Medium", "vehicles": "FOTON Ollin / Aumark Urban & Regional Delivery", "markets": ["Southeast Asia", "South America"], "formula": "N91 (Mineral-Carbon Eco)"}),
]


def classify_disc_views(files: list[str]) -> dict[str, Any]:
    """Identify friction face (front), backing plate stamping (back), and side profile views.
    Note: Studio shoots name the stamped backing plate '*-正' and the friction lining face '*-反'.
    For 5200、5300, '反面.png' is the friction face and '正面图.png' is the backing plate."""
    front = None
    back = None
    side = None
    sorted_files = sorted(files)
    for f in sorted_files:
        fname = f.rsplit("/", 1)[-1]
        if ("反" in fname) and not front:
            front = f
        elif ("正" in fname) and not back:
            back = f
        elif ("侧" in fname) and not side:
            side = f
    if not front and sorted_files:
        front = sorted_files[0]
    if not back:
        back = sorted_files[1] if len(sorted_files) > 1 else front
    if not side:
        side = sorted_files[-1] if sorted_files else front
    return {"front": front, "back": back, "side": side, "all": sorted_files}


def classify_drum_views(files: list[str]) -> dict[str, Any]:
    """Identify primary & detail views for drum brake linings."""
    sorted_files = sorted(files)
    front = sorted_files[0] if sorted_files else None
    back = sorted_files[1] if len(sorted_files) > 1 else front
    side = sorted_files[-1] if len(sorted_files) > 2 else back
    return {"front": front, "back": back, "side": side, "all": sorted_files}


def match_drum_rule(folder_name: str) -> dict[str, Any]:
    for prefix, spec in DRUM_RULES:
        if prefix in folder_name:
            return spec
    return {
        "wva": folder_name,
        "gys": f"GYS-{re.sub(r'[^A-Za-z0-9]+', '', folder_name)[:8] or 'CUSTOM'}",
        "size": "205 × 180 × 15.5",
        "length_mm": 205.0,
        "width_mm": 180.0,
        "thickness_mm": 15.5,
        "holes": 10,
        "axle": "Universal Commercial Axle",
        "vehicles": f"Commercial Fleet Replacement ({folder_name})",
        "markets": ["Southeast Asia", "Middle East", "South America"],
        "formula": "GY-HD400",
    }


def make_slug(text: str) -> str:
    s = text.lower().strip()
    s = re.sub(r"[^a-z0-9]+", "-", s).strip("-")
    return s or "model"


def build_catalog(
    drum_files: dict[str, list[str]],
    disc_files: dict[str, list[str]],
    brand_renders: list[dict[str, str]],
    samples: list[str],
    videos: list[str],
    pdf_assets: dict[str, list[dict[str, str]]],
) -> dict[str, Any]:
    items: list[dict[str, Any]] = []

    # Build disc items first (high priority export models)
    for folder in sorted(disc_files.keys()):
        files = disc_files[folder]
        spec = DISC_SPECS.get(folder, {
            "wva": folder,
            "gys": f"GYD-{folder}",
            "size": "247.5 × 109.5 × 30.0",
            "length_mm": 247.5,
            "width_mm": 109.5,
            "thickness_mm": 30.0,
            "holes": 0,
            "caliper": "Air Disc Caliper",
            "vehicles": f"Commercial Disc Brake Assembly ({folder})",
            "axle": "22.5\" Air Disc Axle",
            "markets": ["Europe", "Middle East", "South America"],
            "formula": "GY-HD400",
        })
        views = classify_disc_views(files)
        items.append({
            "id": f"disc-{make_slug(spec['gys'])}-{make_slug(folder)}",
            "folder": folder,
            "category": "disc",
            "category_label": "Air Disc Brake Pad",
            "wva": spec["wva"],
            "gys": spec["gys"],
            "size": spec["size"],
            "length_mm": spec["length_mm"],
            "width_mm": spec["width_mm"],
            "thickness_mm": spec["thickness_mm"],
            "holes": spec["holes"],
            "axle": spec["axle"],
            "caliper": spec.get("caliper", ""),
            "vehicles": spec["vehicles"],
            "markets": spec["markets"],
            "formula": spec["formula"],
            "friction_mu": "0.35–0.38",
            "certification": "IATF 16949:2016 · ECE R90",
            "views": views,
        })

    # Build drum items
    for folder in sorted(drum_files.keys()):
        files = drum_files[folder]
        spec = match_drum_rule(folder)
        views = classify_drum_views(files)
        # Disambiguate A/B or Front/Rear suffix in display wva
        display_wva = spec["wva"]
        if folder not in display_wva and ("A" in folder[-2:] or "B" in folder[-2:] or "320" in folder or "冠英之星" in folder):
            display_wva = f"{spec['wva']} ({folder})"
        items.append({
            "id": f"drum-{make_slug(spec['gys'])}-{len(items)+1}",
            "folder": folder,
            "category": "drum",
            "category_label": "Heavy-Duty Drum Lining",
            "wva": display_wva,
            "gys": spec["gys"],
            "size": spec["size"],
            "length_mm": spec["length_mm"],
            "width_mm": spec["width_mm"],
            "thickness_mm": spec["thickness_mm"],
            "holes": spec["holes"],
            "axle": spec["axle"],
            "caliper": "S-Cam Drum Brake",
            "vehicles": spec["vehicles"],
            "markets": spec["markets"],
            "formula": spec["formula"],
            "friction_mu": "0.35–0.38",
            "certification": "IATF 16949:2016 · GB5763-2018",
            "views": views,
        })

    catalog = {
        "company": {
            "brand": "KOOYING",
            "legal_name": "Zhejiang Kooying Autoparts Co., Ltd.",
            "legal_name_zh": "浙江冠英汽车零部件有限公司",
            "founded": 2006,
            "huzhou_plant_year": 2019,
            "investment": "RMB 180M",
            "annual_capacity": "15,000,000 Sets/Year",
            "export_countries": "30+",
            "sub_brands": ["KOOYING", "GUANLONG 冠隆", "XINLAI 新莱", "SHENGDIAO 圣雕", "SANAN 三安", "KANENG 卡能"],
            "certifications": ["IATF 16949:2016", "ECE R90 (E-Mark)", "GB5763-2018", "CCC 3C"],
            "dyno_metrics": {
                "friction_coefficient": "0.35–0.38",
                "wear_400c_kooying_mm": 0.040,
                "wear_400c_benchmark_mm": 0.085,
                "constant_speed_weight_loss_g": 0.580,
                "density_g_cm3": 2.040,
                "hardness_hrm": 8.442,
                "impact_strength_j_cm2": 0.340,
                "noise_hz": "<= 1.6 Hz",
                "formula_series": "3 Series / 30+ Asbestos-Free Formulas",
            },
        },
        "stats": {
            "total_models": len(items),
            "disc_models": sum(1 for x in items if x["category"] == "disc"),
            "drum_models": sum(1 for x in items if x["category"] == "drum"),
            "total_disc_photos": sum(len(v) for v in disc_files.values()),
            "total_drum_photos": sum(len(v) for v in drum_files.values()),
        },
        "brand_renders": brand_renders,
        "samples": samples,
        "videos": videos,
        "factory_assets": pdf_assets["factory"],
        "factory_clean": pdf_assets.get("factory_clean", []),
        "drawing_assets": pdf_assets["drawings"],
        "items": items,
    }
    return catalog


def main() -> None:
    DATA_OUT.mkdir(parents=True, exist_ok=True)
    print("1/4 Extracting & deduplicating drum brake pad archive...")
    drum_files = extract_drum_zip()
    print(f"    -> Drum models: {len(drum_files)}, photos: {sum(len(v) for v in drum_files.values())}")

    print("2/4 Extracting disc brake pad archive (3-view photos, 5-brand renders, MP4 video)...")
    disc_files, brand_renders, samples, videos = extract_disc_zip()
    print(f"    -> Disc models: {len(disc_files)}, photos: {sum(len(v) for v in disc_files.values())}, brand renders: {len(brand_renders)}, samples: {len(samples)}, videos: {len(videos)}")

    print("3/4 Rendering factory, R&D lab & catalog drawing assets from official PDFs...")
    pdf_assets = render_pdf_assets()
    print(f"    -> Factory assets: {len(pdf_assets['factory'])}, Drawings: {len(pdf_assets['drawings'])}")

    print("4/4 Writing products-catalog.json and products-catalog.js...")
    catalog = build_catalog(drum_files, disc_files, brand_renders, samples, videos, pdf_assets)

    json_path = DATA_OUT / "products-catalog.json"
    with open(json_path, "w", encoding="utf-8") as f:
        json.dump(catalog, f, ensure_ascii=False, indent=2)

    js_path = DATA_OUT / "products-catalog.js"
    with open(js_path, "w", encoding="utf-8") as f:
        f.write("window.KOOYING_CATALOG = ")
        json.dump(catalog, f, ensure_ascii=False)
        f.write(";\n")

    print(f"Done! Total indexed models: {catalog['stats']['total_models']} (Disc: {catalog['stats']['disc_models']}, Drum: {catalog['stats']['drum_models']})")


if __name__ == "__main__":
    main()
