const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const model = vm.createContext({});
vm.runInContext(fs.readFileSync(`${__dirname}/../Model.js`, 'utf8').replace('.pragma library', ''), model);
const plain = value => JSON.parse(JSON.stringify(value));

test('links retain query parameters and escape attributes and surrounding markup', () => {
  assert.equal(model.linkify('<b> https://example.com/?q=test&page=2.'), '&lt;b&gt; <a href="https://example.com/?q=test&amp;page=2">https://example.com/?q=test&amp;page=2</a>.');
  assert.equal(model.linkify('javascript:alert(1)'), 'javascript:alert(1)');
  assert.equal(model.linkify('https://example.com/" onclick="bad'), '<a href="https://example.com/">https://example.com/</a>&quot; onclick=&quot;bad');
  assert.equal(model.linkify('https://example.com/?token=a&signature=b&expires=3'), '<a href="https://example.com/?token=a&amp;signature=b&amp;expires=3">https://example.com/?token=a&amp;signature=b&amp;expires=3</a>');
});

test('identical text from the other person does not consume a pending send', () => {
 const pending={id:'tx',tmpID:'tx',conversationID:'a',fromMe:true,text:'OK',provisional:true,timestamp:1};
 const incoming={id:'in',conversationID:'a',fromMe:false,text:'OK',timestamp:2};
 assert.deepEqual(plain(model.mergeMessage([pending], incoming)), [pending,incoming]);
});

test('same-text sends reconcile independently, regardless of acknowledgement order', () => {
 const pending=id=>({id,tmpID:id,conversationID:'a',fromMe:true,text:'OK',provisional:true,pending:true,timestamp:1});
 const real={id:'server2',tmpID:'tx2',conversationID:'a',fromMe:true,text:'OK',pending:false,timestamp:2};
 let list=model.mergeMessage([pending('tx1'),pending('tx2')],real);
 list=model.mergeMessage(list,pending('tx2'));
 assert.equal(list.length,2);
 assert.equal(list[1].id,'server2');
 list=model.failSend(list,'tx1');
 assert.equal(list[0].failed,true);
 assert.equal(list[1].pending,false);
});

test('real updates keep their transaction identity and cannot cross conversations', () => {
 const original={id:'server',tmpID:'tx',conversationID:'a',fromMe:true,timestamp:1};
 const update={id:'server',conversationID:'a',fromMe:true,timestamp:1,delivery:'read'};
 const merged=model.mergeMessage([original],update);
 assert.equal(merged[0].tmpID,'tx');
 assert.equal(model.mergeMessage(merged,{...original,conversationID:'b'}).length,2);
});

test('metadata-free live update preserves an already rendered attachment', () => {
 const image={id:'server',conversationID:'a',fromMe:true,timestamp:1,attachments:[{key:'tx',path:'/tmp/photo.jpg',isImage:true}]};
 const update={id:'server',conversationID:'a',fromMe:true,timestamp:2,delivery:'sent'};
 const merged=model.mergeMessage([image],update);
 assert.equal(merged[0].attachments[0].path,'/tmp/photo.jpg');
});

test('generated transaction IDs have UUID format and distinguish consecutive sends', () => {
 const ids=new Set();
 for(let i=0;i<100;i++) {
  const id=model.transactionID();
  assert.match(id,/^[\da-f]{8}-[\da-f]{4}-4[\da-f]{3}-[89ab][\da-f]{3}-[\da-f]{12}$/);
  ids.add(id);
 }
 assert.equal(ids.size,100);
});

test('refresh preserves local sends and replaces them once server history catches up', () => {
 const pending={id:'tx',tmpID:'tx',conversationID:'a',fromMe:true,provisional:true,pending:true,timestamp:1};
 assert.deepEqual(plain(model.refreshMessages([pending], [])), [pending]);
 const real={...pending,id:'server',provisional:false,pending:false};
 assert.deepEqual(plain(model.refreshMessages([pending], [real])), [real]);
});
