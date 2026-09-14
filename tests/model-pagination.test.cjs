const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const model = vm.createContext({});
vm.runInContext(fs.readFileSync(`${__dirname}/../Model.js`, 'utf8').replace('.pragma library', ''), model);
const plain = value => JSON.parse(JSON.stringify(value));

test('older page merge preserves current live records on overlap, deduplicating IDs and sorting by timestamp', () => {
  const current = [
    { id: 'm2', conversationID: 'c1', fromMe: false, text: 'Hello', delivery: 'read', timestamp: 200 },
    { id: 'm3', conversationID: 'c1', fromMe: true, text: 'World', timestamp: 300 }
  ];
  const fetchedOlder = [
    { id: 'm1', conversationID: 'c1', fromMe: false, text: 'First', timestamp: 100 },
    { id: 'm2', conversationID: 'c1', fromMe: false, text: 'Hello', delivery: 'sent', timestamp: 200 }
  ];

  const merged = model.mergePage(current, fetchedOlder, true);
  assert.equal(merged.length, 3);
  assert.deepEqual(plain(merged.map(m => m.id)), ['m1', 'm2', 'm3']);
  assert.equal(merged[1].delivery, 'read', 'current live read receipt must survive older stale page on overlap');
  assert.equal(merged[0].timestamp, 100);
  assert.equal(merged[1].timestamp, 200);
  assert.equal(merged[2].timestamp, 300);
});

test('current live read receipt on outgoing message survives older stale page on overlap', () => {
  const current = [
    { id: 'srv1', tmpID: 'tx1', conversationID: 'c1', fromMe: true, text: 'Delivered msg', delivery: 'read', timestamp: 200 }
  ];
  const olderStale = [
    { id: 'srv0', tmpID: 'tx0', conversationID: 'c1', fromMe: true, text: 'Older msg', delivery: 'sent', timestamp: 100 },
    { id: 'srv1', tmpID: 'tx1', conversationID: 'c1', fromMe: true, text: 'Delivered msg', delivery: 'sent', timestamp: 200 }
  ];

  const merged = model.mergePage(current, olderStale, true);
  assert.equal(merged.length, 2);
  assert.equal(merged[1].id, 'srv1');
  assert.equal(merged[1].delivery, 'read', 'outgoing live read receipt must survive older stale page');
});

test('repeated identical text with distinct IDs are preserved independently without collapsing', () => {
  const current = [
    { id: 'msg-b', conversationID: 'c1', fromMe: false, text: 'Repeat text', timestamp: 20 },
    { id: 'msg-c', conversationID: 'c1', fromMe: true, tmpID: 'tx-c', text: 'Repeat text', timestamp: 30 }
  ];
  const fetchedOlder = [
    { id: 'msg-a', conversationID: 'c1', fromMe: false, text: 'Repeat text', timestamp: 10 },
    { id: 'msg-b', conversationID: 'c1', fromMe: false, text: 'Repeat text', timestamp: 20 }
  ];

  const mergedOlder = model.mergePage(current, fetchedOlder, true);
  assert.equal(mergedOlder.length, 3);
  assert.deepEqual(plain(mergedOlder.map(m => m.id)), ['msg-a', 'msg-b', 'msg-c']);
  assert.deepEqual(plain(mergedOlder.map(m => m.text)), ['Repeat text', 'Repeat text', 'Repeat text']);

  const freshPage = [
    { id: 'msg-c', conversationID: 'c1', fromMe: true, tmpID: 'tx-c', text: 'Repeat text', timestamp: 30 },
    { id: 'msg-d', conversationID: 'c1', fromMe: false, text: 'Repeat text', timestamp: 40 }
  ];
  const mergedRefresh = model.mergePage(mergedOlder, freshPage, false);
  assert.equal(mergedRefresh.length, 4);
  assert.deepEqual(plain(mergedRefresh.map(m => m.id)), ['msg-a', 'msg-b', 'msg-c', 'msg-d']);
});

test('pending tx reconciles to real ack during older page merge', () => {
  const pending = { id: 'tx-100', tmpID: 'tx-100', conversationID: 'c1', fromMe: true, text: 'Sending...', provisional: true, pending: true, timestamp: 250 };
  const current = [
    pending,
    { id: 'm-live', conversationID: 'c1', fromMe: false, text: 'Reply', timestamp: 300 }
  ];
  const fetchedOlder = [
    { id: 'm-old', conversationID: 'c1', fromMe: false, text: 'Older', timestamp: 100 },
    { id: 'srv-100', tmpID: 'tx-100', conversationID: 'c1', fromMe: true, text: 'Sending...', provisional: false, pending: false, timestamp: 250 }
  ];

  const merged = model.mergePage(current, fetchedOlder, true);
  assert.equal(merged.length, 3);
  assert.deepEqual(plain(merged.map(m => m.id)), ['m-old', 'srv-100', 'm-live']);
  const reconciled = merged.find(m => m.tmpID === 'tx-100');
  assert.ok(reconciled);
  assert.equal(reconciled.id, 'srv-100');
  assert.equal(reconciled.provisional, false);
  assert.equal(reconciled.pending, false);
});

test('pending tx reconciles to real ack during latest-page refresh', () => {
  const pending = { id: 'tx-200', tmpID: 'tx-200', conversationID: 'c1', fromMe: true, text: 'Provisional msg', provisional: true, pending: true, timestamp: 200 };
  const current = [
    { id: 'm-hist', conversationID: 'c1', fromMe: false, text: 'Earlier', timestamp: 100 },
    pending
  ];
  const fetchedLatest = [
    { id: 'srv-200', tmpID: 'tx-200', conversationID: 'c1', fromMe: true, text: 'Provisional msg', provisional: false, pending: false, timestamp: 200 },
    { id: 'm-fresh', conversationID: 'c1', fromMe: false, text: 'Latest fresh', timestamp: 300 }
  ];

  const merged = model.mergePage(current, fetchedLatest, false);
  assert.equal(merged.length, 3);
  assert.deepEqual(plain(merged.map(m => m.id)), ['m-hist', 'srv-200', 'm-fresh']);
  const reconciled = merged.find(m => m.tmpID === 'tx-200');
  assert.ok(reconciled);
  assert.equal(reconciled.id, 'srv-200');
  assert.equal(reconciled.provisional, false);
  assert.equal(reconciled.pending, false);
});

test('latest-page refresh after several older pages retains loaded history and applies fresh records', () => {
  const makeMsg = (id, ts, text, extra = {}) => ({
    id: `msg-${id}`,
    conversationID: 'c1',
    fromMe: id % 2 === 0,
    text: text || `Msg ${id}`,
    timestamp: ts,
    ...extra
  });

  // Page 1: latest initial load (messages 30-39)
  const page1 = [];
  for (let i = 30; i <= 39; i++) page1.push(makeMsg(i, i * 1000));
  let messages = model.mergePage([], page1, false);
  assert.equal(messages.length, 10);

  // Page 2: older page (messages 15-32, overlapping 30-32)
  const page2 = [];
  for (let i = 15; i <= 32; i++) page2.push(makeMsg(i, i * 1000));
  messages = model.mergePage(messages, page2, true);
  assert.equal(messages.length, 25);
  assert.equal(messages[0].id, 'msg-15');
  assert.equal(messages[messages.length - 1].id, 'msg-39');

  // Page 3: even older page (messages 1-18, overlapping 15-18)
  const page3 = [];
  for (let i = 1; i <= 18; i++) page3.push(makeMsg(i, i * 1000));
  messages = model.mergePage(messages, page3, true);
  assert.equal(messages.length, 39);
  assert.equal(messages[0].id, 'msg-1');
  assert.equal(messages[messages.length - 1].id, 'msg-39');

  // Page 4: latest refresh occurs, returning messages 35-45 (new 40-45, overlapping 35-39 with updated status on 38)
  const refreshPage = [];
  for (let i = 35; i <= 45; i++) {
    refreshPage.push(makeMsg(i, i * 1000, undefined, i === 38 ? { delivery: 'read' } : {}));
  }

  const refreshed = model.mergePage(messages, refreshPage, false);

  // History from page 2 and page 3 (messages 1 to 34) must be retained
  assert.equal(refreshed.length, 45);
  assert.equal(refreshed[0].id, 'msg-1');
  assert.equal(refreshed[refreshed.length - 1].id, 'msg-45');
  // Overlapping record 38 must have the updated read receipt applied
  assert.equal(refreshed.find(m => m.id === 'msg-38').delivery, 'read');
  // Messages must be strictly sorted by timestamp
  for (let i = 0; i < refreshed.length; i++) {
    assert.equal(refreshed[i].id, `msg-${i + 1}`);
    if (i > 0) {
      assert.ok(refreshed[i].timestamp >= refreshed[i - 1].timestamp);
    }
  }
});

test('input arrays are not mutated by mergePage', () => {
  const currentItem = { id: 'm2', conversationID: 'c1', fromMe: false, text: 'B', timestamp: 20 };
  const fetchedItem = { id: 'm1', conversationID: 'c1', fromMe: false, text: 'A', timestamp: 10 };

  const current = Object.freeze([Object.freeze({ ...currentItem })]);
  const fetched = Object.freeze([Object.freeze({ ...fetchedItem })]);

  const olderResult = model.mergePage(current, fetched, true);
  assert.equal(olderResult.length, 2);
  assert.equal(current.length, 1);
  assert.equal(fetched.length, 1);
  assert.deepEqual(plain(current), [currentItem]);
  assert.deepEqual(plain(fetched), [fetchedItem]);

  const refreshResult = model.mergePage(current, fetched, false);
  assert.equal(refreshResult.length, 2);
  assert.equal(current.length, 1);
  assert.equal(fetched.length, 1);
  assert.deepEqual(plain(current), [currentItem]);
  assert.deepEqual(plain(fetched), [fetchedItem]);
});

test('empty page merges preserve current records and advance without error', () => {
  const current = [
    { id: 'm1', conversationID: 'c1', fromMe: false, text: 'Existing', timestamp: 100 }
  ];

  // Older empty page (e.g. empty page with advancing cursor)
  const mergedOlderEmpty = model.mergePage(current, [], true);
  assert.deepEqual(plain(mergedOlderEmpty), current);

  // Latest refresh empty page
  const mergedRefreshEmpty = model.mergePage(current, [], false);
  assert.deepEqual(plain(mergedRefreshEmpty), current);

  // Initial load with empty current
  const initialLoad = model.mergePage([], current, false);
  assert.deepEqual(plain(initialLoad), current);

  // Both empty
  const bothEmpty = model.mergePage([], [], true);
  assert.deepEqual(plain(bothEmpty), []);
});

test('messages from different conversations are not merged or deduplicated together', () => {
  const conv1 = { id: 'msg-1', conversationID: 'conv-1', fromMe: true, tmpID: 'tx-1', text: 'Hey', timestamp: 10 };
  const conv2 = { id: 'msg-1', conversationID: 'conv-2', fromMe: true, tmpID: 'tx-1', text: 'Hey', timestamp: 20 };

  const merged = model.mergePage([conv1], [conv2], true);
  assert.equal(merged.length, 2);
  assert.equal(merged[0].conversationID, 'conv-1');
  assert.equal(merged[1].conversationID, 'conv-2');
});


test('duplicates within one fetched page do not create duplicate bubbles', () => {
 const m={id:'duplicate',conversationID:'a',fromMe:false,timestamp:5};
 for (const older of [true,false]) {
  const result=model.mergePage([], [m,{...m}], older);
  assert.equal(result.length,1);
 }
});
