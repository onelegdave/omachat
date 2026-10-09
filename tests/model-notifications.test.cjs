const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const policy = vm.createContext({});
vm.runInContext(fs.readFileSync(`${__dirname}/../Notifications.js`, 'utf8').replace('.pragma library', ''), policy);
const now = 1800000000000000;
const incoming = {id:'message',conversationID:'chat',text:'Hello',timestamp:now};

test('only new incoming, displayable messages qualify for notifications', () => {
  assert.equal(policy.isIncoming(incoming, now-1000000, now), true);
  for (const field of ['fromMe','deleted','provisional','pending','failed']) {
    assert.equal(policy.isIncoming({...incoming,[field]:true}, now-1000000, now), false);
  }
  for (const value of [{}, {...incoming,id:''}, {...incoming,conversationID:''}, {...incoming,text:''}, {...incoming,timestamp:NaN}, {...incoming,timestamp:now-2000000}, {...incoming,timestamp:now+120000000}]) {
    assert.equal(policy.isIncoming(value, now-1000000, now), false);
  }
  assert.equal(policy.isIncoming({...incoming,text:'',attachments:[{isAudio:true}]}, now-1000000, now), true);
});

test('deduplication keys distinguish services, conversations, and delimiter-like IDs', () => {
  const keys = [
    policy.messageKey('telegram', incoming),
    policy.messageKey('whatsapp', incoming),
    policy.messageKey('telegram', {...incoming,conversationID:'another'}),
    policy.messageKey('telegram', {...incoming,conversationID:'chat","message'}),
    policy.messageKey('telegram', {...incoming,id:'message","chat'})
  ];
  assert.equal(new Set(keys).size, keys.length);
});
