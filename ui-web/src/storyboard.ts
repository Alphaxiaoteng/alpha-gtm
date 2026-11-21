export interface StoryboardCard {
  id: string;
  duration: string;
  visual: string;
  shot: string;
  narration: string;
  asset: string;
}

const columnAliases = {
  id: ['镜号', '序号', '编号', 'shotno', 'shotnumber'],
  duration: ['时长', '时间', '时间段', 'duration'],
  visual: ['画面', '场景', '画面内容', 'visual', 'scene'],
  shot: ['景别', '景别运动', '镜头', '机位', 'shottype'],
  narration: ['旁白', '旁白字幕', '台词', '口播', '字幕', 'narration', 'dialogue'],
  asset: ['素材', '所需素材', '素材需求', 'asset', 'assets'],
} as const;

function splitMarkdownRow(line: string) {
  const trimmed = line.trim().replace(/^\|/, '').replace(/\|$/, '');
  const cells: string[] = [];
  let current = '';
  let escaped = false;
  for (const character of trimmed) {
    if (escaped) {
      current += character;
      escaped = false;
    } else if (character === '\\') {
      escaped = true;
    } else if (character === '|') {
      cells.push(current.trim());
      current = '';
    } else {
      current += character;
    }
  }
  if (escaped) current += '\\';
  cells.push(current.trim());
  return cells;
}

function normalizeHeader(value: string) {
  return value.toLowerCase().replace(/[\s_（）()：:·/\-]/g, '');
}

function columnIndex(headers: string[], aliases: readonly string[]) {
  return headers.findIndex((header) => aliases.includes(header));
}

function isDelimiterRow(cells: string[]) {
  return cells.length > 1 && cells.every((cell) => /^:?-{3,}:?$/.test(cell.replace(/\s/g, '')));
}

export function parseStoryboardMarkdownTable(markdown: string): StoryboardCard[] {
  const lines = markdown.split(/\r?\n/);
  for (let headerLine = 0; headerLine < lines.length - 1; headerLine += 1) {
    if (!lines[headerLine].includes('|') || !lines[headerLine + 1].includes('|')) continue;
    const headers = splitMarkdownRow(lines[headerLine]).map(normalizeHeader);
    const delimiter = splitMarkdownRow(lines[headerLine + 1]);
    if (!isDelimiterRow(delimiter)) continue;

    const indexes = {
      id: columnIndex(headers, columnAliases.id),
      duration: columnIndex(headers, columnAliases.duration),
      visual: columnIndex(headers, columnAliases.visual),
      shot: columnIndex(headers, columnAliases.shot),
      narration: columnIndex(headers, columnAliases.narration),
      asset: columnIndex(headers, columnAliases.asset),
    };
    if (indexes.id < 0 || indexes.visual < 0) continue;

    const cards: StoryboardCard[] = [];
    for (let rowLine = headerLine + 2; rowLine < lines.length && cards.length < 100; rowLine += 1) {
      if (!lines[rowLine].includes('|')) break;
      const cells = splitMarkdownRow(lines[rowLine]);
      const valueAt = (index: number) => index >= 0 ? (cells[index] ?? '').trim() : '';
      const id = valueAt(indexes.id);
      const visual = valueAt(indexes.visual);
      if (!id && !visual) continue;
      cards.push({
        id: id || String(cards.length + 1),
        duration: valueAt(indexes.duration),
        visual,
        shot: valueAt(indexes.shot),
        narration: valueAt(indexes.narration),
        asset: valueAt(indexes.asset),
      });
    }
    return cards;
  }
  return [];
}
