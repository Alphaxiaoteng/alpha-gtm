#!/usr/bin/env node
/**
 * Generates the bundled pixel-art Bot avatars.
 *
 * Design rules (product requirement):
 *   - pixel art, 16x16 grid, crisp edges, no gradients
 *   - one avatar per *responsibility*, never per user-chosen name
 *   - four female and four male variants so any bot can match its owner's intent
 *
 * Run: node scripts/gen-bot-avatars.mjs
 * Output: public/bot-avatars/<role>.svg (also copied into the Go embed dir by
 * `npm run build` + the dist sync step).
 */
import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const outDir = join(here, '..', 'public', 'bot-avatars');
mkdirSync(outDir, { recursive: true });

// 16x16 grids. Legend: . background, H hair, S skin, E eye, M mouth,
// B shirt, W eye highlight, C cheek.
const FEMALE = [
  '................',
  '......HHHH......',
  '....HHHHHHHH....',
  '...HHHHHHHHHH...',
  '...HHHHHHHHHH...',
  '..HHHHHHHHHHHH..',
  '..HHSSSSSSSSHH..',
  '..HSSSSSSSSSSH..',
  '..HSWESSSSEWSH..',
  '..HSSSSSSSSSSH..',
  '..HCSSSMMSSSCH..',
  '..HHSSSSSSSSHH..',
  '...HHBBBBBBHH...',
  '..BBBBBBBBBBBB..',
  '.BBBBBBBBBBBBBB.',
  '.BBBBBBBBBBBBBB.',
];

const MALE = [
  '................',
  '................',
  '.....HHHHHH.....',
  '...HHHHHHHHHH...',
  '..HHHHHHHHHHHH..',
  '..HHHHHHHHHHHH..',
  '..HHSSSSSSSSHH..',
  '..HSSSSSSSSSSH..',
  '..HSWESSSSEWSH..',
  '..HSSSSSSSSSSH..',
  '..HCSSSMMSSSCH..',
  '..HHSSSSSSSSHH..',
  '...HSSBBBBSSH...',
  '..BBBBBBBBBBBB..',
  '.BBBBBBBBBBBBBB.',
  '.BBBBBBBBBBBBBB.',
];

/** role, gender, label, palette */
const AVATARS = [
  { role: 'planner',  gender: 'female', label: '选题策划', bg: '#FFF0F5', hair: '#F06C9B', shirt: '#8E6FD8' },
  { role: 'writer',   gender: 'female', label: '文案写作', bg: '#FFF6E5', hair: '#E8894A', shirt: '#4E8FD8' },
  { role: 'designer', gender: 'female', label: '视觉设计', bg: '#F3ECFF', hair: '#9B6BE8', shirt: '#E86A9B' },
  { role: 'support',  gender: 'female', label: '客服运营', bg: '#FFF9DB', hair: '#E0B040', shirt: '#4FB88A' },
  { role: 'reviewer', gender: 'male',   label: '脚本审核', bg: '#EAF2FF', hair: '#3A5FA8', shirt: '#5E7BB8' },
  { role: 'analyst',  gender: 'male',   label: '数据分析', bg: '#E6F7F9', hair: '#2F6B7A', shirt: '#4A9BAA' },
  { role: 'engineer', gender: 'male',   label: '技术开发', bg: '#EAF7EE', hair: '#3F6B4A', shirt: '#5AA878' },
  { role: 'manager',  gender: 'male',   label: '项目管理', bg: '#F0F1F4', hair: '#4A4F5A', shirt: '#6B7280' },
];

const SKIN = '#FFD9B8';
const EYE = '#2B2B3A';
const MOUTH = '#C96B6B';
const CHEEK = '#FFB3C6';

/**
 * Emit merged horizontal runs so a 16x16 grid stays a few hundred bytes
 * instead of 256 rects.
 */
function rects(grid, colors) {
  const out = [];
  for (let y = 0; y < grid.length; y += 1) {
    const row = grid[y];
    let x = 0;
    while (x < row.length) {
      const ch = row[x];
      let run = 1;
      while (x + run < row.length && row[x + run] === ch) run += 1;
      const fill = colors[ch];
      if (fill) {
        out.push(`<rect x="${x}" y="${y}" width="${run}" height="1" fill="${fill}"/>`);
      }
      x += run;
    }
  }
  return out.join('');
}

for (const avatar of AVATARS) {
  const grid = avatar.gender === 'female' ? FEMALE : MALE;
  const colors = {
    H: avatar.hair,
    S: SKIN,
    E: EYE,
    W: '#FFFFFF',
    M: MOUTH,
    C: CHEEK,
    B: avatar.shirt,
  };
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" shape-rendering="crispEdges" role="img" aria-label="${avatar.label}像素头像"><rect width="16" height="16" fill="${avatar.bg}"/>${rects(grid, colors)}</svg>`;
  writeFileSync(join(outDir, `${avatar.role}.svg`), `${svg}\n`);
  console.log(`wrote ${avatar.role}.svg (${avatar.label}, ${avatar.gender})`);
}

console.log(`\n${AVATARS.length} avatars -> ${outDir}`);
