const hashtagPattern = /#[^#\s,，;；、。.!！?？:：()[\]{}<>《》“”"'‘’…/\\|]+/gu;
const wordCharacter = /[\p{L}\p{N}_]/u;

export function extractContentTags(text: string): string[] {
  const tags: string[] = [];
  const seen = new Set<string>();
  let previousTokenEnd = -1;

  for (const match of text.matchAll(hashtagPattern)) {
    const index = match.index ?? 0;
    const previousCharacter = index > 0 ? text[index - 1] : '';
    if (wordCharacter.test(previousCharacter) && index !== previousTokenEnd) continue;

    const tag = match[0].slice(1).trim();
    previousTokenEnd = index + match[0].length;
    if (!tag || seen.has(tag)) continue;
    seen.add(tag);
    tags.push(tag);
  }

  return tags;
}
