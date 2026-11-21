"""按 alphafde.cn 深色主题生成渐变导航 logo。
渐变与动画参数取自 https://alphafde.cn/assets/css/style.css
.brand__logo-img: mask = alpha-fde-nav-logo-black.png
background = linear-gradient(90deg,#ffffff,#f1f5f9,#a5b4fc,#818cf8,#c084fc,#38bdf8,#e2e8f0,#ffffff)
"""
from PIL import Image
import numpy as np

mask = Image.open("alphafde-nav-mask.png").convert("L")
w, h = mask.size

# 深色主题渐变停止点（与官网一致）
stops = [
    (0.00, (255, 255, 255)),
    (0.10, (241, 245, 249)),
    (0.25, (165, 180, 252)),
    (0.38, (129, 140, 248)),
    (0.52, (192, 132, 252)),
    (0.68, (56, 189, 248)),
    (0.84, (226, 232, 240)),
    (1.00, (255, 255, 255)),
]

# 逐像素横向插值
xs = np.linspace(0, 1, w)
grad = np.zeros((w, 3), dtype=np.float32)
for i in range(3):
    vals = np.interp(
        xs,
        [s[0] for s in stops],
        [s[1][i] for s in stops],
    )
    grad[:, i] = vals

# 竖向铺满，再用 mask 做 alpha（形状 h×w×3）
rgb = np.tile(grad[None, :, :], (h, 1, 1)).astype(np.uint8)
alpha = np.array(mask)
rgba = np.dstack([rgb, alpha])
out = Image.fromarray(rgba, "RGBA")

# 放大到 3x，精细文字在落地页上更清晰
scale = 3
out = out.resize((w * scale, h * scale), Image.LANCZOS)
out.save("alphafde-gradient-logo.png")
print("saved", out.size)
