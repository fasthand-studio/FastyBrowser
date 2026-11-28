package main

// pageScript runs in every top-level page. It reports title, address, icon and history state,
// turns window.open and target=_blank links into new tabs, and forwards the browser's shortcuts
// (the keys the Linux build binds) so they work while a page has the focus.
const pageScript = `(function(){
if(window.top!==window||window.__fasty)return;window.__fasty=1;
function post(o){try{window.chrome.webview.postMessage(JSON.stringify(o))}catch(e){}}
function icon(){var l=document.querySelector('link[rel~="icon"],link[rel="shortcut icon"],link[rel="apple-touch-icon"]');
 if(l&&l.href)return l.href;return /^https?:/.test(location.protocol)?location.origin+'/favicon.ico':''}
function meta(){var n=window.navigation;post({a:'meta',t:document.title,u:location.href,i:icon(),
 cb:n?!!n.canGoBack:history.length>1,cf:n?!!n.canGoForward:false})}
var last='';function tick(){var k=document.title+'|'+location.href+'|'+icon();if(k!==last){last=k;meta()}}
var ps=history.pushState,rs=history.replaceState;
history.pushState=function(){var r=ps.apply(this,arguments);tick();return r};
history.replaceState=function(){var r=rs.apply(this,arguments);tick();return r};
addEventListener('popstate',tick);addEventListener('hashchange',tick);
addEventListener('load',function(){last='';tick()});
addEventListener('beforeunload',function(){post({a:'loading',on:true})});
document.addEventListener('DOMContentLoaded',function(){last='';tick();
 var t=document.querySelector('title');if(t)new MutationObserver(tick).observe(t,{childList:true,characterData:true,subtree:true});
 new MutationObserver(function(){tick()}).observe(document.head,{childList:true,subtree:false,attributes:false})});
window.open=function(u){if(u){try{post({a:'open',u:new URL(String(u),location.href).href})}catch(e){}}return null};
document.addEventListener('click',function(e){if(e.defaultPrevented||e.button!==0)return;
 var a=e.target&&e.target.closest&&e.target.closest('a[href]');
 if(a&&(a.target==='_blank'||e.ctrlKey||e.shiftKey)&&/^https?:/i.test(a.href)){e.preventDefault();post({a:'open',u:a.href})}},true);
var KEYS={'ctrl+t':1,'ctrl+shift+t':1,'ctrl+shift+n':1,'ctrl+w':1,'ctrl+l':1,'ctrl+k':1,'ctrl+d':1,'ctrl+y':1,
 'ctrl+alt+b':1,'ctrl+shift+b':1,'ctrl+shift+j':1,'ctrl+,':1,'ctrl+r':1,'f5':1,'ctrl+shift+r':1,'ctrl+shift+s':1,'ctrl+s':1,
 'ctrl+tab':1,'ctrl+shift+tab':1,'alt+arrowleft':1,'alt+arrowright':1,'ctrl+bracketleft':1,'ctrl+bracketright':1,
 'ctrl+shift+bracketleft':1,'ctrl+shift+bracketright':1,'f11':1,'ctrl+shift+c':1,'ctrl+alt+r':1,'ctrl+f5':1,
 'ctrl+1':1,'ctrl+2':1,'ctrl+3':1,'ctrl+4':1,'ctrl+5':1,'ctrl+6':1,'ctrl+7':1,'ctrl+8':1,'ctrl+9':1};
addEventListener('keydown',function(e){
 var k=/^Bracket/.test(e.code)?e.code.toLowerCase():e.key.toLowerCase();
 var c=(e.ctrlKey?'ctrl+':'')+(e.altKey?'alt+':'')+(e.shiftKey?'shift+':'')+k;
 if(KEYS[c]){e.preventDefault();post({a:'key',k:c})}
},true);
tick();})();`
