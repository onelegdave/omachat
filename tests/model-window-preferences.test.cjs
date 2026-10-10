const test = require('node:test');
const assert = require('node:assert/strict');
const {load} = require('./qml-functions.cjs');
function service() {return {requests:[],call(method,params,callback,network) {this.requests.push({method,params,callback,network});}};}
function panel(s) {
 const values={chatViewInFlight:null,savingChatView:false,pendingChatViewChanges:[],chatViewRevision:0,chatViewGeneration:0,configRequest:0,chatViewLoaded:false,lastConversations:{},sidebarCollapsed:false,serviceTabs:[],service:s,popoutOpen:false,panelConfigLoaded:false,openPendingConfig:false,configLoadPending:false,configGeneration:0,alwaysPopout:false,popoutMode:'floating',opened:false};
 values.controller={show(){values.opened=true;},hide(){values.opened=false;}};
 values.openPopout=()=>{values.popoutOpen=true;};values.setPopoutMode=mode=>{values.popoutMode=mode;};values.closePopout=()=>{values.popoutOpen=false;};
 return load('Panel.qml',['open','close','toggle','loadConfig','applyWindowPreferences','suspendChatViewWrites','queueChatViewChange'],values);
}
const config={alwaysPopout:false,popoutMode:'floating',uiScale:1};
test('closing or toggling cancels a pending open even after a successful config reply',()=>{
 for(const cancel of ['close','toggle']) {
  const s=service(),p=panel(s);p.open();assert.equal(p.openPendingConfig,true);p[cancel]();s.requests.pop().callback(true,config);assert.equal(p.opened,false);assert.equal(p.openPendingConfig,false);
 }
});
test('a retired A-B-A panel config read cannot overwrite the current lifetime',()=>{
 const a=service(),b=service(),p=panel(a);p.loadConfig();const old=a.requests.pop().callback;
 p.service=b;p.configGeneration++;p.configLoadPending=false;p.loadConfig();p.service=a;p.configGeneration++;p.configLoadPending=false;p.loadConfig();
 old(true,{alwaysPopout:true,popoutMode:'tiled'});assert.equal(p.panelConfigLoaded,false);assert.equal(p.alwaysPopout,false);assert.equal(p.configLoadPending,true);
 a.requests.pop().callback(true,config);assert.equal(p.panelConfigLoaded,true);
});
test('a saved panel preference wins over older configuration reads',()=>{
 const s=service(),p=panel(s);p.loadConfig();const stale=s.requests.pop().callback;p.applyWindowPreferences(true,'tiled');stale(true,config);assert.equal(p.alwaysPopout,true);assert.equal(p.popoutMode,'tiled');
});
test('failed config opens recovery without a repeated retry loop',()=>{
 const s=service(),p=panel(s);p.open();const req=s.requests.pop();req.callback(false,'offline');assert.equal(p.opened,true);assert.equal(p.panelConfigLoaded,false);assert.equal(p.openPendingConfig,false);assert.equal(s.requests.length,0);
});
function settings(s) {return load('SettingsView.qml',['load','saveWindowPreferences','retireWindowRequests'],{service:s,savingWindowPreferences:false,windowRequestGeneration:0,alwaysPopout:false,popoutMode:'floating',windowStatusText:'',windowPreferencesSaved(){},telegramConfigured:false,telegramApiId:0,uiScale:1});}
test('window preference save invalidates an older Settings config reply',()=>{
 const s=service(),v=settings(s);v.load();const old=s.requests.pop().callback;v.saveWindowPreferences(true,'tiled');s.requests.pop().callback(true,{alwaysPopout:true,popoutMode:'tiled'});old(true,config);assert.equal(v.alwaysPopout,true);assert.equal(v.popoutMode,'tiled');
});
test('malformed window-save receipts preserve the acknowledged setting',()=>{
 const s=service(),v=settings(s);v.alwaysPopout=true;v.popoutMode='tiled';v.saveWindowPreferences(false,'floating');s.requests.pop().callback(true,{});assert.equal(v.alwaysPopout,true);assert.equal(v.popoutMode,'tiled');assert.match(v.windowStatusText,/could not be confirmed/);
});
test('retired save cannot change preferences after a service lifetime change',()=>{
 const a=service(),b=service(),v=settings(a);v.saveWindowPreferences(true,'tiled');const old=a.requests.pop().callback;v.service=b;v.windowRequestGeneration++;v.savingWindowPreferences=false;v.service=a;v.windowRequestGeneration++;old(true,{alwaysPopout:true,popoutMode:'tiled'});assert.equal(v.alwaysPopout,false);assert.equal(v.savingWindowPreferences,false);
});

test('socket loss releases a pending config read and keeps recovery reachable',()=>{
 const s=service(),p=panel(s);p.open();const stale=s.requests.pop().callback;
 s.connected=false;p.suspendChatViewWrites();assert.equal(p.opened,true);assert.equal(p.configLoadPending,false);
 s.connected=true;p.loadConfig();const fresh=s.requests.pop().callback;
 stale(true,{alwaysPopout:true,popoutMode:'tiled'});assert.equal(p.configLoadPending,true);assert.equal(p.panelConfigLoaded,false);
 fresh(true,config);assert.equal(p.configLoadPending,false);assert.equal(p.panelConfigLoaded,true);assert.equal(p.alwaysPopout,false);
});
test('socket loss retires a Settings save even when the same service reconnects',()=>{
 const s=service(),v=settings(s);s.connected=true;v.saveWindowPreferences(true,'tiled');const stale=s.requests.pop().callback;
 s.connected=false;v.retireWindowRequests();v.load();assert.equal(s.requests.length,0);assert.equal(v.savingWindowPreferences,false);
 s.connected=true;v.load();s.requests.pop().callback(true,config);stale(true,{alwaysPopout:true,popoutMode:'tiled'});
 assert.equal(v.alwaysPopout,false);assert.equal(v.popoutMode,'floating');
});

test('retired pop-out layout replies cannot apply a superseded mode or reopened window',()=>{
 const timer={requestedMode:'floating',restarts:0,restart(){this.restarts++;}};
 const p=load('Panel.qml',['requestPopoutMode','acceptPopoutReply'],{popoutOpen:true,popoutLayoutRevision:0,popoutModeError:'',appliedPopoutMode:'',popoutMapAttempts:0,modeMapTimer:timer});
 p.requestPopoutMode('floating');const old=p.popoutLayoutRevision;p.requestPopoutMode('tiled');
 assert.equal(p.acceptPopoutReply(old),false);assert.equal(timer.requestedMode,'tiled');assert.equal(p.acceptPopoutReply(p.popoutLayoutRevision),true);
 p.popoutOpen=false;p.popoutLayoutRevision++;const retries=timer.restarts;assert.equal(p.acceptPopoutReply(old),false);assert.equal(timer.restarts,retries);
 p.popoutOpen=true;p.requestPopoutMode('floating');assert.equal(p.acceptPopoutReply(old),false);
});
