import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const source = await readFile(new URL('../src/components/LocalContentView.tsx', import.meta.url), 'utf8');
const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');

test('local content editor can select ordered media from the existing library', () => {
  assert.match(source, /api\.getMediaLibrary/);
  assert.match(source, /data-testid="local-content-library-media-select"/);
  assert.match(source, /data-testid="local-content-library-media-add"/);
  assert.match(source, /pendingLibraryIDs/);
  assert.match(source, /contentAPI\.attach\(saved\.id, libraryID, 'content', position\)/);
  assert.match(source, /item\.purpose === 'graph_text' \|\| item\.purpose === 'article'/);
});

test('video content can select a separate image cover from the library', () => {
  assert.match(source, /data-testid="local-content-library-cover-select"/);
  assert.match(source, /selectedCoverLibraryID/);
  assert.match(source, /contentAPI\.attach\(saved\.id, selectedCoverLibraryID, 'cover', 0\)/);
  assert.match(api, /role: 'content' \| 'cover'/);
  assert.match(api, /position: number/);
});
