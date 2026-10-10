const test = require('node:test');
const assert = require('node:assert/strict');
const {load} = require('./qml-functions.cjs');
function service() {
  return {requests:[],call(method,params,callback,network) { this.requests.push({method,params,callback,network}); },isServiceEnabled(){return true;},stateFor(){return 'connected';},conversationsFor(){return [];}};
}
function manager(s) {
  return load('DesktopNotifications.qml',['retireConnection','applyConfig','savePreferences','test','forgetNetwork','flush'],
    {service:s,connected:true,loaded:true,enabled:true,previews:false,saving:false,sending:false,errorText:'',generation:0,configRevision:0,sendRequest:0,networkEpochs:{},seen:[],views:[],queue:[],Qt:{callLater(fn){fn();}}});
}
test('reconnect retires notification save, send and test callbacks',()=>{
  for (const method of ['save','send','test']) {
    const s=service(), n=manager(s);
    if(method==='save')n.savePreferences({previews:true});
    else if(method==='test')n.test();
    else { n.queue=[{network:'telegram',params:{message:{conversationID:'demo'}}}];n.flush(); }
    const stale=s.requests.pop().callback;
    n.connected=false;n.retireConnection();n.connected=true;n.retireConnection();
    n.applyConfig({notificationsEnabled:false,notificationPreviews:false});
    stale(true,{notificationsEnabled:true,notificationPreviews:true});
    assert.equal(n.enabled,false);assert.equal(n.previews,false);assert.equal(n.errorText,'');assert.equal(n.saving,false);assert.equal(n.sending,false);
  }
});
test('stale config and malformed saves cannot overwrite notification preferences',()=>{
  const s=service(),n=manager(s), revision=n.configRevision;
  n.savePreferences({previews:true});s.requests.pop().callback(true,{notificationsEnabled:true,notificationPreviews:true});
  assert.equal(n.applyConfig({notificationsEnabled:false,notificationPreviews:false},revision),false);assert.equal(n.previews,true);
  n.savePreferences({previews:false});s.requests.pop().callback(true,{});
  assert.equal(n.previews,true);assert.match(n.errorText,/could not be confirmed/);
});
test('forgotten account send reply cannot introduce a new-account error',()=>{
  const s=service(),n=manager(s);n.queue=[{network:'telegram',params:{message:{conversationID:'demo'}}}];n.flush();
  const callback=s.requests.pop().callback;n.forgetNetwork('telegram');callback(false,'old account');
  assert.equal(n.errorText,'');assert.equal(n.sending,false);
});
function panel(s) {
  return load('Panel.qml',['queueChatViewChange','saveChatView','flushChatViewChanges','retireChatView','suspendChatViewWrites'],{sharedDraftStore:{drafts:{}},unifiedPreferenceGeneration:0,savingUnifiedInboxPreference:false,openPendingConfig:false,configGeneration:0,configLoadPending:false,panelConfigLoaded:false,service:s,savingChatView:false,chatViewInFlight:null,chatViewRevision:0,chatViewGeneration:0,configRequest:0,pendingChatViewChanges:[],pendingConversationID:'',lastConversations:{},chatViewLoaded:false,chatViewError:''});
}
test('offline chat preferences coalesce per service without losing newest fields',()=>{
  const s=service();s.connected=false;const p=panel(s);
  for(let i=0;i<1000;i++)p.saveChatView({sidebarCollapsed:i%2===0},'telegram');
  p.saveChatView({conversationID:'new'},'telegram');p.saveChatView({lastService:'whatsapp'},'whatsapp');
  assert.equal(p.pendingChatViewChanges.length,2);
  s.connected=true;p.flushChatViewChanges();assert.equal(s.requests.length,1);assert.equal(s.requests[0].params.sidebarCollapsed,false);assert.equal(s.requests[0].params.conversationID,'new');
  s.requests[0].callback(true);assert.equal(s.requests.length,2);assert.equal(s.requests[1].params.lastService,'whatsapp');
});
test('retired A-B-A chat-view reply cannot release or flush a new service write',()=>{
  const a=service(),b=service();a.connected=b.connected=true;const p=panel(a);
  p.saveChatView({conversationID:'old'},'telegram');const old=a.requests.pop().callback;
  p.service=b;p.retireChatView();p.service=a;p.retireChatView();p.saveChatView({conversationID:'new'},'telegram');
  p.pendingChatViewChanges=[{network:'telegram',params:{sidebarCollapsed:true}}];old(false,'old failure');
  assert.equal(p.savingChatView,true);assert.equal(p.chatViewError,'');assert.equal(a.requests.length,1);
});
test('saved restoration avoids missing chats while preserving an explicit in-session selection',()=>{
 const values={detailOnly:false,detailConversationID:'',composer:{},selectedConvID:'',_selectedByNet:{},network:'telegram',lastConversations:{telegram:'missing'},conversations:[],selectConversation(id){values.selectedConvID=id;}};
 const inbox=load('InboxView.qml',['restoreConversation'],values);
 inbox.restoreConversation();assert.equal(inbox.selectedConvID,'');
 inbox._selectedByNet={telegram:'draft-chat'};inbox.restoreConversation();assert.equal(inbox.selectedConvID,'draft-chat');
});
test('an inactive-account preference update cannot restore an older global service choice',()=>{
 const s=service();s.connected=false;const p=panel(s);
 p.saveChatView({lastService:'telegram'},'telegram');p.saveChatView({lastService:'whatsapp'},'whatsapp');p.saveChatView({conversationID:''},'telegram');
 s.connected=true;p.flushChatViewChanges();s.requests[0].callback(true);
 const choices=s.requests.map(r=>r.params.lastService).filter(Boolean);assert.deepEqual(choices,['whatsapp']);
});
test('socket loss retries only latest preferences and retires the old callback',()=>{
 const s=service();s.connected=true;const p=panel(s);
 p.saveChatView({lastService:'telegram',conversationID:'old'},'telegram');const old=s.requests[0].callback;
 p.saveChatView({conversationID:'new'},'telegram');p.saveChatView({lastService:'whatsapp'},'whatsapp');
 s.connected=false;p.suspendChatViewWrites();assert.equal(p.pendingChatViewChanges.length,2);
 s.connected=true;p.flushChatViewChanges();assert.equal(s.requests[1].params.conversationID,'new');assert.equal(s.requests[1].params.lastService,undefined);
 old(false,'retired');assert.equal(p.savingChatView,true);assert.equal(p.chatViewError,'');assert.equal(s.requests.length,2);
 s.requests[1].callback(true);assert.equal(s.requests[2].params.lastService,'whatsapp');
});
