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
/* picture in picture: Chromium's own, started from a small button on a video or with Ctrl+Shift+P */
function bigVideo(){var v=[].slice.call(document.querySelectorAll('video')).filter(function(x){return x.readyState>0&&!x.disablePictureInPicture});
 v.sort(function(a,b){return b.videoWidth*b.videoHeight-a.videoWidth*a.videoHeight});return v[0]}
function pip(){if(!document.pictureInPictureEnabled)return;
 if(document.pictureInPictureElement){document.exitPictureInPicture().catch(function(){});return}
 var v=bigVideo();if(v)v.requestPictureInPicture().catch(function(){})}
window.__fastyPip=pip;
var pb=null,pt=0;
function pipBtn(v){if(!document.pictureInPictureEnabled||v.disablePictureInPicture)return;
 if(!pb){pb=document.createElement('button');pb.type='button';pb.setAttribute('aria-label','Picture in picture');pb.title='Picture in picture (Ctrl+Shift+P)';
  pb.style.cssText='position:fixed;z-index:2147483646;width:30px;height:30px;border-radius:8px;border:0;background:rgba(23,23,23,.72);color:#fff;cursor:pointer;padding:0;display:none';
  pb.innerHTML='<svg viewBox="0 0 16 16" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.5" style="display:block;margin:auto"><rect x="1.5" y="3" width="13" height="10" rx="2"/><rect x="8" y="8" width="5" height="3.5" rx=".8" fill="currentColor"/></svg>';
  pb.addEventListener('click',function(e){e.preventDefault();e.stopPropagation();pip()});document.documentElement.appendChild(pb)}
 var r=v.getBoundingClientRect();if(r.width<160||r.height<90){return}
 pb.style.left=Math.max(4,r.right-38)+'px';pb.style.top=Math.max(4,r.top+8)+'px';pb.style.display='block';clearTimeout(pt);pt=setTimeout(function(){if(pb)pb.style.display='none'},2200)}
document.addEventListener('mousemove',function(e){var t=e.target;if(t&&t.tagName==='VIDEO')pipBtn(t)},true);
/* hide something: click an element to hide it on this site */
var pk=null,pkEl=null;
function sel(el){if(el.id&&/^[A-Za-z][\w-]*$/.test(el.id)&&document.querySelectorAll('#'+el.id).length===1)return '#'+el.id;
 var path=[];while(el&&el.nodeType===1&&el!==document.documentElement&&path.length<5){var s=el.tagName.toLowerCase();
  var cls=[].slice.call(el.classList).filter(function(c){return /^[A-Za-z][\w-]*$/.test(c)&&c.length<30}).slice(0,2);if(cls.length)s+='.'+cls.join('.');
  var sib=el.parentNode?[].slice.call(el.parentNode.children).filter(function(x){return x.tagName===el.tagName}):[];if(sib.length>1)s+=':nth-of-type('+(sib.indexOf(el)+1)+')';
  path.unshift(s);var q=path.join(' > ');try{if(document.querySelectorAll(q).length===1)return q}catch(e){}el=el.parentNode}return path.join(' > ')}
function pickStop(){if(pk){pk.remove();pk=null}document.removeEventListener('mousemove',pickMove,true);document.removeEventListener('click',pickClick,true);document.removeEventListener('keydown',pickKey,true);document.documentElement.style.cursor=''}
function pickMove(e){var el=document.elementFromPoint(e.clientX,e.clientY);if(!el||el===pk)return;pkEl=el;var r=el.getBoundingClientRect();
 pk.style.cssText='position:fixed;z-index:2147483646;pointer-events:none;border:2px solid #171717;background:rgba(53,132,228,.18);border-radius:4px;left:'+r.left+'px;top:'+r.top+'px;width:'+r.width+'px;height:'+r.height+'px'}
function pickClick(e){e.preventDefault();e.stopPropagation();var el=pkEl;pickStop();if(!el||el===document.body||el===document.documentElement)return;
 var label=(el.getAttribute('aria-label')||el.innerText||el.tagName).replace(/\s+/g,' ').trim().slice(0,60);post({a:'veil',v:sel(el),t:label})}
function pickKey(e){if(e.key==='Escape'){e.preventDefault();pickStop()}}
window.__fastyPick=function(){if(pk)return;pk=document.createElement('div');document.documentElement.appendChild(pk);document.documentElement.style.cursor='crosshair';
 document.addEventListener('mousemove',pickMove,true);document.addEventListener('click',pickClick,true);document.addEventListener('keydown',pickKey,true)};
var KEYS={'ctrl+t':1,'ctrl+shift+t':1,'ctrl+shift+n':1,'ctrl+w':1,'ctrl+l':1,'ctrl+k':1,'ctrl+d':1,'ctrl+y':1,
 'ctrl+alt+b':1,'ctrl+shift+b':1,'ctrl+shift+a':1,'ctrl+shift+h':1,'ctrl+shift+u':1,'ctrl+shift+j':1,'ctrl+,':1,'ctrl+r':1,'f5':1,'ctrl+shift+r':1,'ctrl+shift+s':1,'ctrl+s':1,
 'ctrl+tab':1,'ctrl+shift+tab':1,'alt+arrowleft':1,'alt+arrowright':1,'ctrl+bracketleft':1,'ctrl+bracketright':1,
 'ctrl+shift+bracketleft':1,'ctrl+shift+bracketright':1,'f11':1,'ctrl+shift+c':1,'ctrl+alt+r':1,'ctrl+f5':1,
 'ctrl+1':1,'ctrl+2':1,'ctrl+3':1,'ctrl+4':1,'ctrl+5':1,'ctrl+6':1,'ctrl+7':1,'ctrl+8':1,'ctrl+9':1};
addEventListener('keydown',function(e){
 var k=/^Bracket/.test(e.code)?e.code.toLowerCase():e.key.toLowerCase();
 var c=(e.ctrlKey?'ctrl+':'')+(e.altKey?'alt+':'')+(e.shiftKey?'shift+':'')+k;
 if(c==='ctrl+shift+p'){e.preventDefault();pip();return}
 if(KEYS[c]){e.preventDefault();post({a:'key',k:c})}
},true);
tick();})();`
