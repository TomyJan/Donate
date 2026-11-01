import { test } from 'node:test';
import assert from 'node:assert/strict';
import { metadata } from './version.mjs';

test('exact tag links identify the release while non-tag builds keep the commit suffix', () => {
  const commit = 'a'.repeat(40);
  assert.deepEqual(metadata({ commit, exactTag: 'v1.2.3' }), { version: '1.2.3', tag: 'v1.2.3', commit });
  assert.deepEqual(metadata({ commit, nearestTag: 'v1.2.3' }), { version: '1.2.3', tag: '', commit });
});
test('release refuses dirty, untagged, conflicting or malformed versions', () => {
  const base = { commit: 'a'.repeat(40), release: true, exactTag: 'v1.2.3', version: '1.2.3' };
  assert.equal(metadata(base).tag, 'v1.2.3');
  for (const override of [
    { dirty: true },
    { exactTag: '' },
    { version: '1.2.4' },
    { version: '1;echo unsafe' },
  ])
    assert.throws(() => metadata({ ...base, ...override }));
});
