// 逐页截取 deck 的每一屏（页数由 DOM 里的 .slide 数量决定，不写死）
const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const dir = __dirname;
const src = path.join(dir, 'deploy', 'index.html');
const outDir = path.join(dir, 'shots');
const chrome = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const tmp = path.join(dir, 'shots', '_view.html');

const orig = fs.readFileSync(src, 'utf8');
const total = (orig.match(/<section class="slide/g) || []).length;
if (!total) throw new Error('no .slide found in ' + src);
fs.mkdirSync(outDir, { recursive: true });

for (let n = 0; n < total; n++) {
  const injected = orig.replace(
    "pageno.textContent='1 / '+slides.length;",
    `go(${n});pageno.textContent=(cur+1)+' / '+slides.length;`
  );
  fs.writeFileSync(tmp, injected);
  const png = path.join(outDir, `slide-${String(n + 1).padStart(2, '0')}.png`);
  execSync(
    `"${chrome}" --headless=new --disable-gpu --hide-scrollbars ` +
    `--force-device-scale-factor=1 --virtual-time-budget=3000 ` +
    `--screenshot="${png}" --window-size=1600,900 "file://${tmp}" 2>/dev/null`
  );
  console.log('shot', n + 1, '/', total, '->', png);
}
fs.unlinkSync(tmp);
