const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const keyboard = vm.createContext({});
vm.runInContext(fs.readFileSync(`${__dirname}/../Keybindings.js`, 'utf8').replace('.pragma library', ''), keyboard);
const plain = value => JSON.parse(JSON.stringify(value));

test('default action registry documents stable ids and sensible bindings', () => {
  const actions = plain(keyboard.actions());
  const byId = Object.fromEntries(actions.map(action => [action.id, action]));
  assert.equal(byId.help.defaultShortcut, '?');
  assert.equal(byId.search.defaultShortcut, '/');
  assert.equal(byId.compose.defaultShortcut, 'i');
  assert.equal(byId.nextUnread.defaultShortcut, 'u');
  assert.ok(byId.settings && byId.newConversation);
});

test('shift-letter shortcuts remain stable after normalization and resolve from real key codes', () => {
  assert.equal(keyboard.normalizeShortcut('Ctrl+Shift+p'), 'Ctrl+Shift+p');
  assert.equal(keyboard.resolve({key: 80, text: 'p', ctrl: true, shift: true}, 'navigation', {}).id, 'help');
});

test('action search matches labels and keywords while excluding unavailable services', () => {
  assert.deepEqual(plain(keyboard.actionsForQuery('unread', {}, ['gmessages'])).map(action => action.id), ['nextUnread']);
  assert.equal(plain(keyboard.actionsForQuery('whatsapp', {}, ['gmessages'])).length, 0);
  assert.equal(plain(keyboard.actionsForQuery('', {}, ['gmessages'])).some(action => action.id === 'service.whatsapp'), false);
});

test('printable navigation shortcuts are ignored by editors while modified shortcuts remain available', () => {
  assert.equal(keyboard.match('i', {text:'i'}, 'navigation'), true);
  assert.equal(keyboard.match('i', {text:'i'}, 'editing'), false);
  assert.equal(keyboard.match('Ctrl+N', {text:'n',ctrl:true}, 'editing'), true);
  assert.equal(keyboard.match('?', {text:'?',shift:true}, 'editing'), false);
  assert.equal(keyboard.match('?', {text:'?',shift:true}, 'navigation'), true);
});

test('shortcut normalization preserves shifted punctuation and aliases', () => {
  assert.equal(keyboard.normalizeShortcut('control + shift + P'), 'Ctrl+Shift+p');
  assert.equal(keyboard.normalizeShortcut('Shift+?'), '?');
  assert.equal(keyboard.normalizeShortcut('alt+left'), '');
  assert.equal(keyboard.normalizeShortcut('Ctrl+Tab'), '');
});

test('recording ignores modifier-only and unsupported key events', () => {
  assert.equal(keyboard.eventShortcut({key: 0x01000020, shift: true}), '');
  assert.equal(keyboard.eventShortcut({key: 0x01000021, ctrl: true}), '');
  assert.equal(keyboard.normalizeShortcut('Ctrl+Whatever'), '');
  assert.equal(keyboard.normalizeShortcut('Ctrl+Ctrl+x'), '');
});

test('override validation rejects unknown actions, reserved chords, duplicates, and context conflicts', () => {
  assert.equal(keyboard.validateOverrides({unknown:'x'}).ok, false);
  assert.equal(keyboard.validateOverrides({settings:'Ctrl+Tab'}).ok, false);
  assert.equal(keyboard.validateOverrides({search:'x',compose:'x'}).ok, false);
  assert.equal(keyboard.validateOverrides({search:'i'}).ok, false);
  assert.equal(keyboard.validateOverrides({search:'Ctrl+F',settings:'Ctrl+,'}).ok, true);
});

test('resolve returns one available action and never dispatches ambiguous overrides', () => {
  assert.equal(keyboard.resolve({text:'i'}, 'navigation', {}).id, 'compose');
  assert.equal(keyboard.resolve({text:'i'}, 'editing', {}).id, '');
  assert.equal(keyboard.resolve({text:'n',ctrl:true}, 'editing', {}).id, 'newConversation');
  assert.equal(keyboard.resolve({text:'1'}, 'navigation', {enabledServices:['gmessages']}).id, 'service.gmessages');
  assert.equal(keyboard.resolve({text:'2'}, 'navigation', {enabledServices:['gmessages']}).id, '');
});
