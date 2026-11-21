# show/

湖州黑客松 AlphaGTM 的全套交付物。评委看这份就够了。

```
show/
  alphagtm/  落地页（独立单文件，部署 gtm.alphafde.cn）
  deck/      路演 PDF（8 页）+ deck.html 源文件
  poster/    80×200cm 易拉宝 PNG/PDF + 二维码
  copy/      逐页口播稿与待核对清单
  landing/   humanizer-zh 单页（已作废，保留备查）
  logo/      alphaGTM / AlphaFDE 标识（SVG + PNG）
  video/     演示视频（待录）
  assets/    截图、冠英素材、配图
```

## 落地页（alphagtm/index.html）

独立单文件，logo 以 base64 内联，无外部依赖。可直接部署到 `gtm.alphafde.cn`。
视觉取自 alphagame.club 的 `tokens-bone.css`（骨黑 / 象牙白 / 亮红 / Bodoni + Montserrat）。
两张产品界面图为示意，已在图注标明，不冒充真实运行界面。

```bash
~/.local/bin/preview-serve.sh show 8798
# http://127.0.0.1:8812/alphagtm/index.html
```

## 易拉宝（poster/）

规格 80×200cm @150dpi：PNG 4724×11811，PDF 2268×5669pt。
右下角二维码指向 `https://gtm.alphafde.cn`。

```bash
cd show/poster
python3 make-qr.py        # 重新生成二维码（中文字体用 Hiragino Sans GB）
# 改 poster.html 后重出 PNG/PDF：
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new \
  --disable-gpu --hide-scrollbars --force-device-scale-factor=1 \
  --screenshot=alphaGTM-poster.png --window-size=4724,11811 "file://$PWD/poster.html"
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new \
  --disable-gpu --no-pdf-header-footer --virtual-time-budget=5000 \
  --print-to-pdf=alphaGTM-poster.pdf "file://$PWD/poster.html"
```

## 出路演 PDF

```bash
cd show/deck
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
  --headless=new --disable-gpu --no-pdf-header-footer \
  --virtual-time-budget=4000 \
  --print-to-pdf=alphaGTM-huzhou-v1.pdf "file://$PWD/deck.html"
```

页面尺寸由 CSS `@page { size: 1280px 720px }` 决定，输出 960×540pt，正好 16:9。
每页固定 720px 高、超出裁掉，所以改文案时先看页面有没有溢出。
