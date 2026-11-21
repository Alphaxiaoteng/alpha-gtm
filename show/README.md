# show/

湖州黑客松 AlphaGTM 的全套交付物。评委看这份就够了。

```
show/
  deck/   路演 PDF（13 页）+ deck.html 源文件，改字重出 PDF
  copy/   逐页口播稿与待核对清单
  logo/   alphaGTM 标识（SVG + PNG）
  video/  演示视频（待录）
  assets/ 截图、冠英素材、配图
```

## 出 PDF

```bash
cd show/deck
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
  --headless=new --disable-gpu --no-pdf-header-footer \
  --virtual-time-budget=4000 \
  --print-to-pdf=alphaGTM-huzhou-v1.pdf "file://$PWD/deck.html"
```

页面尺寸由 CSS `@page { size: 1280px 720px }` 决定，输出 960×540pt，正好 16:9。
每页固定 720px 高、超出裁掉，所以改文案时先看页面有没有溢出。

## 预览

```bash
~/.local/bin/preview-serve.sh show/deck 8798
```