const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');
const root = path.resolve(__dirname, '..');
function api() {
  const filename = path.join(root, 'EmojiSearch.js');
  const context = vm.createContext({});
  if (fs.existsSync(filename)) vm.runInContext(fs.readFileSync(filename, 'utf8').replace(/^\.pragma library\s*\n/, ''), context);
  assert.equal(typeof context.buildIndex, 'function', 'buildIndex must be available to QML');
  assert.equal(typeof context.search, 'function', 'search must be available to QML');
  return context;
}
const rows = [
  { e: '👍', label: 'thumbs up', keywords: ['hand', 'good'], aliases: ['+1', 'thumbsup'] },
  { e: '👎', label: 'thumbs down', keywords: ['hand', 'bad'], aliases: ['-1', 'thumbsdown'] },
  { e: '😄', label: 'grinning face with smiling eyes', keywords: ['happy'], aliases: ['smile'] },
  { e: '❤️', label: 'red heart', keywords: ['love'], aliases: ['heart'] },
  { e: '🎉', label: 'party popper', keywords: ['celebration'], aliases: ['tada'] },
];
function results(module, index, query) { return Array.from(module.search(index, query)); }
test('empty search browses original records in input order without mutating them', () => {
  const module = api();
  const frozen = rows.map(row => Object.freeze({ ...row }));
  const index = module.buildIndex(Object.freeze(frozen));
  assert.deepEqual(results(module, index, '  '), frozen);
  assert.equal(results(module, index, '')[0], frozen[0]);
});
test('picker reopening follows the keep-previous-search preference', () => {
  const module = api();
  assert.equal(module.searchTextForOpen('heart', false), '');
  assert.equal(module.searchTextForOpen('heart', true), 'heart');
  assert.equal(module.searchTextForOpen('', true), '');
});
test('grid Up returns to search only from its first row', () => {
  const module = api();
  assert.equal(module.gridUpReturnsToSearch(0, 8), true);
  assert.equal(module.gridUpReturnsToSearch(7, 8), true);
  assert.equal(module.gridUpReturnsToSearch(8, 8), false);
  assert.equal(module.gridUpReturnsToSearch(23, 8), false);
  assert.equal(module.gridUpReturnsToSearch(-1, 8), true);
  assert.equal(module.gridUpReturnsToSearch(0, 0), true);
  assert.equal(module.gridIndexForFocus(8, 20), 8);
  assert.equal(module.gridIndexForFocus(-1, 20), 0);
  assert.equal(module.gridIndexForFocus(20, 20), 0);
  assert.equal(module.gridIndexForFocus(8, 0), -1);
});
test('exact labels, aliases, glyphs and keywords normalize without conflating signed aliases', () => {
  const module = api();
  const index = module.buildIndex(rows);
  for (const query of ['thumbs up', 'THUMBS_UP', 'thumbs-up', ':thumbsup:', ':+1:', '👍'])
    assert.equal(results(module, index, query)[0], rows[0], query);
  assert.equal(results(module, index, ':-1:')[0], rows[1]);
  assert.equal(results(module, index, 'love')[0], rows[3]);
  assert.equal(results(module, index, '❤')[0], rows[3]);
  assert.equal(results(module, index, 'tada')[0], rows[4]);
  assert.deepEqual(results(module, index, 'definitelynomatch'), []);
});
test('rank exact names and duplicate aliases before prefixes, tokens and substrings deterministically', () => {
  const module = api();
  const fixture = [
    { e: '1', label: 'big heart', aliases: [] },
    { e: '2', label: 'heartfelt', aliases: [] },
    { e: '3', label: 'heart', aliases: [] },
    { e: '4', label: 'sweetheart', aliases: [] },
    { e: '5', label: 'other', aliases: ['heart'] },
  ];
  const index = module.buildIndex(fixture);
  assert.deepEqual(results(module, index, 'heart'), [fixture[2], fixture[4], fixture[1], fixture[0], fixture[3]]);
  assert.deepEqual(results(module, index, 'heart'), results(module, index, 'heart'));
});
test('multiword queries require all words across fields and support reversed word order and legacy k', () => {
  const module = api();
  const fixture = rows.concat([{ e: 'X', k: 'old happy waving hand' }]);
  const index = module.buildIndex(fixture);
  assert.equal(results(module, index, 'good hand')[0], rows[0]);
  assert.equal(results(module, index, 'up thumbs')[0], rows[0]);
  assert.deepEqual(results(module, index, 'thumbs celebration'), []);
  assert.equal(results(module, index, 'happy hand')[0], fixture[5]);
});
test('bounded Damerau matching handles transposition, insertion, deletion and substitution after literal matches', () => {
  const module = api();
  const index = module.buildIndex(rows);
  for (const query of ['smiel', 'smle', 'smiile', 'smilex', 'smilez', 'smipe'])
    assert.equal(results(module, index, query)[0], rows[2], query);
  assert.equal(results(module, index, 'thmbs hand')[0], rows[0]);
  const literal = { e: 'X', label: 'smielish' };
  assert.equal(results(module, module.buildIndex(rows.concat([literal])), 'smiel')[0], literal);
  assert.deepEqual(results(module, index, 'smxxe'), []);
  assert.equal(results(module, index, 'smi')[0], rows[2]);
  assert.deepEqual(results(module, index, 'sml'), []);
  assert.deepEqual(results(module, index, '1'), [rows[0], rows[1]]);
  assert.deepEqual(results(module, index, '+2'), []);
});
test('bundled data is complete, unique by presentation-insensitive identity, and searches real upstream vocabulary', () => {
  const dataPath = path.join(root, 'data/emoji-search.json');
  assert.ok(fs.existsSync(dataPath), 'offline emoji dataset must be bundled');
  const data = JSON.parse(fs.readFileSync(dataPath, 'utf8'));
  assert.equal(data.length, 3953);
  const identities = new Set();
  for (const row of data) {
    assert.equal(typeof row.e, 'string');
    assert.ok(row.e.length > 0 && !/[\uFFFD\uD800-\uDBFF](?![\uDC00-\uDFFF])/u.test(row.e));
    assert.ok(row.label.length > 0);
    assert.ok(Array.isArray(row.keywords) && row.keywords.every(x => typeof x === 'string'));
    assert.ok(Array.isArray(row.aliases) && row.aliases.every(x => typeof x === 'string'));
    const id = row.e.replace(/[\uFE0E\uFE0F]/g, '');
    assert.ok(!identities.has(id), `duplicate glyph ${row.e}`);
    identities.add(id);
  }
  const module = api();
  const index = module.buildIndex(data);
  for (const [query, glyph] of [['thumbs up', '👍'], ['thumbsup', '👍'], [':+1:', '👍'], [':-1:', '👎'], ['heart', '❤️'], ['tada', '🎉'], [':joy:', '😂'], ['smiel', '😄']])
    assert.equal(results(module, index, query)[0].e.replace(/[\uFE0E\uFE0F]/g, ''), glyph.replace(/[\uFE0E\uFE0F]/g, ''), query);
  assert.ok(results(module, index, 'love').some(row => row.e === '❤️'));
  assert.ok(results(module, index, 'medium skin tone').some(row => row.e === '👍🏽'));
  assert.equal(results(module, index, '').length, 3953);
});
test('manual generator verifies vendored checksums without network or packages', () => {
  const generator = path.join(root, 'scripts/generate-emoji-data.py');
  assert.ok(fs.existsSync(generator), 'manual reproducible generator must exist');
  const { execFileSync } = require('node:child_process');
  const report = execFileSync('python3', [generator, '--check'], { cwd: root, encoding: 'utf8' });
  assert.match(report, /3953 records/);
});
test('bundled license notices distinguish metadata from restricted artwork', () => {
  const docs = fs.readFileSync(path.join(root, 'docs/emoji-data.md'), 'utf8');
  assert.match(docs, /a5fc630a91ca42cddf3f4a66492965600fd3bce8/);
  for (const [file, text] of [['EMOJIBASE-LICENSE.txt', 'Copyright (c) 2017-2019 Miles Johnson'], ['UNICODE-LICENSE.txt', 'UNICODE LICENSE V3'], ['CLDR-LICENSE.txt', 'UNICODE LICENSE V3'], ['JOYPIXELS-LICENSE.md', 'JoyPixels Non-Artwork']])
    assert.ok(fs.readFileSync(path.join(root, 'data', file), 'utf8').includes(text), file);
  assert.match(docs, /No .*artwork/i);
});
test('full-data searches remain interactive after indexing once', () => {
  const { performance } = require('node:perf_hooks');
  const module = api();
  const data = JSON.parse(fs.readFileSync(path.join(root, 'data/emoji-search.json'), 'utf8'));
  const start = performance.now();
  const index = module.buildIndex(data);
  const buildMs = performance.now() - start;
  const queries = ['', 't', 'th', 'thumbs up', 'thumbsup', ':+1:', 'heart', 'love', 'tada', ':joy:', 'smiel', 'medium skin tone', 'hand light', 'qzxvbnmasdf'];
  for (const query of queries) module.search(index, query);
  const samples = [];
  for (let round = 0; round < 5; round++) {
    for (const query of queries) {
      const start = performance.now();
      module.search(index, query);
      samples.push(performance.now() - start);
    }
  }
  samples.sort((a, b) => a - b);
  const p95 = samples[Math.ceil(samples.length * 0.95) - 1];
  console.log(`emoji benchmark: records=${data.length}, build=${buildMs.toFixed(1)}ms, searches=${samples.length}, p95=${p95.toFixed(1)}ms, max=${samples.at(-1).toFixed(1)}ms`);
  assert.ok(p95 < 200, `full-data p95 ${p95.toFixed(1)}ms exceeds 200ms regression budget`);
});
