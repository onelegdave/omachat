const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const model = vm.createContext({});
vm.runInContext(fs.readFileSync(`${__dirname}/../Model.js`, 'utf8').replace('.pragma library', ''), model);
const summary = statuses => JSON.parse(JSON.stringify(model.combinedConnectionStatus(statuses)));

test('enabled online providers report connected without a Google dependency', () => {
  assert.deepEqual(summary([{state:'connected'}, {state:'connected', phoneOK:true}]),
    {label:'CONNECTED', connected:true});
});

test('partial availability counts only healthy connected providers', () => {
  for (const unavailable of ['unpaired', 'connecting', 'disconnected', 'error']) {
    assert.deepEqual(summary([{state:'connected'}, {state:unavailable}]),
      {label:'1/2 CONNECTED', connected:false});
  }
  assert.deepEqual(summary([{state:'connected'}, {state:'connected', phoneOK:false}, null]),
    {label:'1/3 CONNECTED', connected:false});
});

test('offline, pairing, and empty aggregate states remain honest', () => {
  assert.equal(summary([]).label, 'SERVICES OFF');
  assert.equal(summary([{state:'unpaired'}, {state:'unpaired'}]).label, 'NOT PAIRED');
  for (const state of ['connecting', 'pairing', 'gaiaPairing']) {
    assert.equal(summary([{state}, {state:'error'}]).label, 'CONNECTING');
  }
  assert.equal(summary([{state:'connected', phoneOK:false}]).label, 'PHONE OFFLINE');
  assert.deepEqual(summary([{state:'disconnected'}, {state:'error',error:'private provider detail'}]),
    {label:'SERVICES OFFLINE', connected:false});
});
