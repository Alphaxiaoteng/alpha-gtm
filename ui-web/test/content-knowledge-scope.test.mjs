import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

test('content assist requires an explicit knowledge base selection', async () => {
  const content = await readFile(new URL('../src/components/ContentView.tsx', import.meta.url), 'utf8');

  assert.match(content, /<option value=\{0\}>请选择知识库<\/option>/);
  assert.match(content, /if \(useKnowledge && !knowledgeBaseID\) \{\s*toast\.show\('请选择知识库', 'error'\);\s*return;/);
  assert.match(content, /disabled=\{assisting \|\| !\(draft\.title \|\| draft\.caption \|\| draft\.body\) \|\| \(assistAction === 'platform_adapt' && !assistPlatform\) \|\| \(useKnowledge && !knowledgeBaseID\)\}/);
});
