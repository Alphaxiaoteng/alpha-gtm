"""纯二维码，无文字无卡片留白。
quiet zone 由 QR 规范决定（4 模块），以保证可扫。
"""
import qrcode
from qrcode.constants import ERROR_CORRECT_H

URL = "https://alphagtm.pages.dev/"

qr = qrcode.QRCode(error_correction=ERROR_CORRECT_H, box_size=30, border=4)
qr.add_data(URL)
qr.make(fit=True)
img = qr.make_image(fill_color="#0A0A0A", back_color="#FFFFFF").convert("RGB")
img.save("alphagtm-qr.png")
print("saved", img.size, "->", URL)
