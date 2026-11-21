const test = require('node:test');
const assert = require('node:assert/strict');
const {load} = require('./qml-functions.cjs');
function service() { return {connected:true,requests:[],call(method,params,callback) { this.requests.push({method,params,callback}); }}; }
function settings(s) {
 return load('SettingsView.qml',['load','saveEmojiSearchPreference','saveUnifiedInboxPreference','saveHistoryPreference','saveDebugLogging','retireWindowRequests'],{
  service:s,preferenceRevision:0,windowRequestGeneration:0,emojiPreferenceGeneration:0,unifiedPreferenceGeneration:0,historyPreferenceGeneration:0,
  savingWindowPreferences:false,savingEmojiSearchPreference:false,savingUnifiedInboxPreference:false,savingHistoryPreference:false,
  debugLoggingGeneration:0,savingDebugLogging:false,debugLogging:false,debugLogPath:'',debugStatusText:'',debugStatusIsError:false,deleteDebugLogConfirmOpen:false,deletingDebugLog:false,
  keepPreviousEmojiSearchText:false,unifiedInboxEnabled:false,whatsappHistoryCacheMB:128,
  emojiSearchPreferenceStatusText:'',unifiedInboxPreferenceStatusText:'',statusText:'',windowStatusText:'',
  emojiSearchPreferenceSaved(){},unifiedInboxPreferenceSaved(){}
 });
}
const cases=[['saveEmojiSearchPreference',true,'keepPreviousEmojiSearchText','savingEmojiSearchPreference'],['saveUnifiedInboxPreference',true,'unifiedInboxEnabled','savingUnifiedInboxPreference'],['saveHistoryPreference',256,'whatsappHistoryCacheMB','savingHistoryPreference'],['saveDebugLogging',true,'debugLogging','savingDebugLogging']];
for (const [method,value,field,busy] of cases) {
 test(`${method} rejects stale config and save replies after reconnect`,()=>{
  const s=service(),v=settings(s);v.load();const oldRead=s.requests.pop().callback;
  v[method](value);const oldSave=s.requests.pop().callback;
  oldRead(true,{[field]:value===true?false:64});assert.equal(v[busy],true);
  s.connected=false;v.retireWindowRequests();s.connected=true;v.load();s.requests.pop().callback(true,{[field]:value===true?false:128});
  oldSave(true,{[field]:value});assert.equal(v[field],value===true?false:128);assert.equal(v[busy],false);
 });
 test(`${method} preserves the prior value on malformed acknowledgement`,()=>{
  const s=service(),v=settings(s);v[method](value);s.requests.pop().callback(true,{});
  assert.equal(v[field],value===true?false:128);assert.equal(v[busy],false);
 });
}
test('debug logging save sends an explicit boolean and never enables on a malformed reply',()=>{
 const s=service(),v=settings(s);v.saveDebugLogging(true);const req=s.requests.pop();
 assert.equal(req.method,'setDebugLogging');assert.equal(JSON.stringify(req.params),'{"enabled":true}');
 req.callback(true,{debugLogging:'yes'});assert.equal(v.debugLogging,false);assert.equal(v.debugStatusIsError,true);
 v.saveDebugLogging(false);assert.equal(JSON.stringify(s.requests.find(r=>r.method==='setDebugLogging').params),'{"enabled":false}');
});
test('history cache rejects values outside the four allowed budgets without an RPC',()=>{
 const s=service(),v=settings(s);for(const value of [0,-1,65,129,1024,NaN])v.saveHistoryPreference(value);assert.equal(s.requests.length,0);
});
test('keyboard save retires a same-service reconnect without releasing a newer save',()=>{
 const s=service();const v=load('KeyboardShortcutsView.qml',['save','retireRequests'],{service:s,saving:false,requestGeneration:0,errorText:'',statusText:'',overrides:{},cancelRecording(){},saved(){}});
 v.save({search:'Ctrl+Alt+f'});const stale=s.requests.pop().callback;v.retireRequests();v.save({search:'Ctrl+Alt+s'});
 stale(true,{keyboardShortcuts:{search:'Ctrl+Alt+f'}});assert.equal(v.saving,true);assert.deepEqual(v.overrides,{});
 s.requests.pop().callback(true,{keyboardShortcuts:{search:'Ctrl+Alt+s'}});assert.equal(v.overrides.search,'Ctrl+Alt+s');assert.equal(v.saving,false);
});
