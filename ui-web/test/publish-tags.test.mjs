import assert from 'node:assert/strict';
import { test } from 'node:test';
import * as tags from '../src/publishTags.ts';

test('publish tags are extracted locally from title and article headings', () => {
  const result = tags.suggestPublishTags(
    '把复杂任务变简单：一个可复用的三步工作流',
    '# 工作流方法\n\n## 明确目标\n\n把任务拆成可以执行的动作，并留下复用模板。',
  );
  assert.equal(result.length, 5);
  assert.ok(result.includes('工作流'));
  assert.ok(result.includes('任务'));
  assert.ok(result.some((tag) => tag.includes('复用')));
  assert.ok(!result.includes('一个'));
});
