const stopWords = new Set(['把', '的', '并', '可', '成', '变', '一个', '一种', '这个', '这些', '那些', '可以', '通过', '进行', '使用', '需要', '就是', '如果', '然后', '我们', '你们', '他们', '什么', '怎么', '已经', '没有', '不是', '为了', '以及']);

function wordTokens(text: string) {
  const segmenter = new Intl.Segmenter('zh-CN', { granularity: 'word' });
  return [...segmenter.segment(text)]
    .filter((part) => part.isWordLike)
    .map((part) => part.segment.trim())
    .filter((word) => word && !stopWords.has(word) && !/^\d+$/u.test(word));
}

function wordCandidates(text: string) {
  const tokens = wordTokens(text);
  const words = tokens.filter((word) => {
    const length = [...word].length;
    return length >= 2 && length <= 12;
  });
  tokens.forEach((word) => {
    if (word.startsWith('可') && [...word].length > 2) words.push([...word].slice(1).join(''));
  });
  // ponytail: only join a one-character Chinese suffix; this recovers terms such
  // as “工作流” without growing an opaque NLP dependency.
  tokens.slice(0, -1).forEach((word, index) => {
    const suffix = tokens[index + 1];
    const combined = `${word}${suffix}`;
    const suffixCompound = [...word].length >= 2 && [...suffix].length === 1;
    const splitWord = [...word].length === 1 && [...suffix].length === 1;
    if ((suffixCompound || splitWord) && [...combined].length <= 6 && /[\p{Script=Han}]/u.test(combined) && text.includes(combined)) {
      words.push(combined);
    }
  });
  return [...new Set(words)];
}

export function suggestPublishTags(title: string, body: string, limit = 5) {
  const score = new Map<string, number>();
  const add = (word: string, weight: number) => score.set(word, (score.get(word) || 0) + weight);
  wordCandidates(title).forEach((word) => add(word, body.includes(word) ? 8 : 6));
  const headings = body.split('\n')
    .filter((line) => /^\s{0,3}#{1,6}\s+/u.test(line))
    .map((line) => line.replace(/^\s{0,3}#{1,6}\s+/u, ''));
  headings.flatMap(wordCandidates).forEach((word) => add(word, 3));
  wordCandidates(body.slice(0, 1600)).forEach((word) => add(word, 1));
  return [...score.entries()]
    .sort((left, right) => right[1] - left[1] || left[0].localeCompare(right[0], 'zh-CN'))
    .slice(0, Math.max(1, Math.min(10, limit)))
    .map(([word]) => word);
}
