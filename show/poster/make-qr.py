"""生成易拉宝右下角二维码，指向 gtm.alphafde.cn。
白底方卡 + 黑码，黑底易拉宝上才扫得出来。
"""
import qrcode
from qrcode.constants import ERROR_CORRECT_H
from PIL import Image, ImageDraw

URL = "https://gtm.alphafde.cn"

qr = qrcode.QRCode(
    version=None,
    error_correction=ERROR_CORRECT_H,
    box_size=12,
    border=2,
)
qr.add_data(URL)
qr.make(fit=True)
code = qr.make_image(fill_color="#0A0A0A", back_color="#FFFFFF").convert("RGB")

# 白底卡片：留白 + 下方一行说明
pad = 46
label_h = 70
card = Image.new(
    "RGB",
    (code.width + pad * 2, code.height + pad * 2 + label_h),
    "#FFFFFF",
)
card.paste(code, (pad, pad))
d = ImageDraw.Draw(card)
# 用系统字体画一行提示
try:
    font = ImageFont = None
    from PIL import ImageFont as _IF
    font = _IF.truetype(
        "/System/Library/Fonts/PingFang.ttc", 26
    )
except Exception:
    font = None
text = "扫码查看 AlphaGTM"
bbox = d.textbbox((0, 0), text, font=font) if font else (0, 0, 300, 26)
tw = bbox[2] - bbox[0]
d.text(
    ((card.width - tw) / 2, code.height + pad * 2 - 4),
    text,
    fill="#0A0A0A",
    font=font,
)
card.save("alphagtm-qr.png")
print("saved", card.size, "->", URL)
