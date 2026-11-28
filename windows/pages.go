package main

import (
	"encoding/json"
	"html"
	"strings"
)

// ChromeHeightDIP is the height of the tab strip plus toolbar, in device-independent pixels.
const ChromeHeightDIP = 88

const baseCSS = `
:root{--bg:#fbf9f5;--card:#fffdfa;--fg:#1b1612;--muted:#6b645d;--line:#e4dfd7;--hover:#f1ede6;--accent:#1b1612}
@media (prefers-color-scheme:dark){:root{--bg:#1c1917;--card:#24201d;--fg:#f4f0ea;--muted:#a39b92;--line:#38322d;--hover:#2c2723;--accent:#f4f0ea}}
*{box-sizing:border-box}html,body{margin:0;height:100%;background:var(--bg);color:var(--fg);font:14px/1.4 "Segoe UI Variable Text","Segoe UI",system-ui,sans-serif;-webkit-user-select:none;user-select:none}
button{font:inherit;color:inherit;background:none;border:0;cursor:pointer}
:focus-visible{outline:2px solid var(--accent);outline-offset:1px}
`

const chromeHTML = `<!doctype html><html><head><meta charset="utf-8"><title>chrome</title><style>` + baseCSS + `
body{display:flex;flex-direction:column;overflow:hidden}
#tabs{display:flex;align-items:flex-end;gap:2px;height:40px;padding:6px 8px 0;overflow-x:auto;overflow-y:hidden;scrollbar-width:none}
.tab{display:flex;align-items:center;gap:6px;min-width:90px;max-width:210px;flex:0 1 210px;height:34px;padding:0 6px 0 12px;border-radius:12px 12px 0 0;color:var(--muted);cursor:default}
.tab:hover{background:var(--hover)}
.tab.on{background:var(--card);color:var(--fg);border:1px solid var(--line);border-bottom:0}
.tab.zz{opacity:.6}
.tab span{flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.tab .x{width:22px;height:22px;border-radius:11px;color:var(--muted);font-size:16px;line-height:20px;flex:none}
.tab .x:hover{background:var(--line);color:var(--fg)}
#new{width:30px;height:30px;border-radius:15px;font-size:18px;color:var(--muted);flex:none;margin-bottom:2px}
#new:hover{background:var(--hover)}
#bar{display:flex;align-items:center;gap:6px;height:48px;padding:0 10px;background:var(--card);border-top:1px solid var(--line);border-bottom:1px solid var(--line)}
.ib{width:34px;height:34px;border-radius:17px;font-size:17px;color:var(--fg);flex:none}
.ib:hover{background:var(--hover)}
#addr{flex:1;min-width:60px;height:34px;border:1px solid var(--line);border-radius:17px;padding:0 14px;background:var(--bg);color:var(--fg);font:inherit;-webkit-user-select:text;user-select:text}
#addr:focus{border-color:var(--accent);outline:0}
#shield{font-size:12px;color:var(--muted);padding:0 8px;white-space:nowrap}
</style></head><body>
<div id="tabs" role="tablist" aria-label="Tabs"></div>
<div id="bar">
<button class="ib" id="back" title="Back (Alt+Left)" aria-label="Back">&#8592;</button>
<button class="ib" id="fwd" title="Forward (Alt+Right)" aria-label="Forward">&#8594;</button>
<button class="ib" id="reload" title="Reload (Ctrl+R)" aria-label="Reload">&#8635;</button>
<input id="addr" type="text" spellcheck="false" autocomplete="off" placeholder="Search or enter an address" aria-label="Address">
<span id="shield" title="Requests blocked by Shield this session"></span>
<button class="ib" id="settings" title="Settings" aria-label="Settings">&#8943;</button>
</div>
<script>
const post=o=>window.chrome.webview.postMessage(JSON.stringify(o));
const $=id=>document.getElementById(id);
let st={tabs:[],active:0};
function render(){
  const box=$('tabs');box.textContent='';
  for(const t of st.tabs){
    const d=document.createElement('div');d.className='tab'+(t.id===st.active?' on':'')+(t.asleep?' zz':'');
    d.setAttribute('role','tab');d.setAttribute('aria-selected',t.id===st.active);d.title=t.title;
    const s=document.createElement('span');s.textContent=t.title||'New tab';d.appendChild(s);
    const x=document.createElement('button');x.className='x';x.textContent='×';x.title='Close tab';x.setAttribute('aria-label','Close tab');
    x.onclick=e=>{e.stopPropagation();post({a:'close',id:t.id})};d.appendChild(x);
    d.onclick=()=>post({a:'switch',id:t.id});
    d.onauxclick=e=>{if(e.button===1)post({a:'close',id:t.id})};
    box.appendChild(d);
  }
  const n=document.createElement('button');n.id='new';n.textContent='+';n.title='New tab (Ctrl+T)';n.setAttribute('aria-label','New tab');n.onclick=()=>post({a:'newtab'});box.appendChild(n);
}
window.fasty={update(s){
  st=s;render();
  if(document.activeElement!==$('addr'))$('addr').value=s.url||'';
  $('shield').textContent=s.shieldOn?('Shield: '+s.blocked+' blocked'):'Shield off';
  if(s.focusAddr){$('addr').focus();$('addr').select()}
}};
$('addr').addEventListener('keydown',e=>{if(e.key==='Enter'){post({a:'nav',v:$('addr').value});e.target.blur()}else if(e.key==='Escape'){e.target.blur();post({a:'sync'})}});
$('addr').addEventListener('focus',e=>e.target.select());
$('back').onclick=()=>post({a:'back'});$('fwd').onclick=()=>post({a:'fwd'});$('reload').onclick=()=>post({a:'reload'});
$('settings').onclick=()=>post({a:'settings'});
addEventListener('keydown',e=>{
  if(e.ctrlKey&&!e.altKey){const k=e.key.toLowerCase();
    if(k==='t'){e.preventDefault();post({a:'newtab'})}
    else if(k==='w'){e.preventDefault();post({a:'closeactive'})}
    else if(k==='l'){e.preventDefault();$('addr').focus();$('addr').select()}
    else if(k==='r'){e.preventDefault();post({a:'reload'})}
    else if(e.key==='Tab'){e.preventDefault();post({a:'cycle',d:e.shiftKey?-1:1})}
  }else if(e.altKey&&e.key==='ArrowLeft'){post({a:'back'})}
  else if(e.altKey&&e.key==='ArrowRight'){post({a:'fwd'})}
  else if(e.key==='F5'){e.preventDefault();post({a:'reload'})}
});
post({a:'ready',w:innerWidth,h:innerHeight,r:devicePixelRatio});
</script></body></html>`

// pageScript runs in every top-level page. It reports title and address changes, turns
// window.open and target=_blank links into new tabs, and forwards browser shortcuts.
const pageScript = `(function(){
if(window.top!==window||window.__fasty)return;window.__fasty=1;
function post(o){try{window.chrome.webview.postMessage(JSON.stringify(o))}catch(e){}}
function meta(){post({a:'meta',t:document.title,u:location.href})}
var last='';function tick(){var k=document.title+'|'+location.href;if(k!==last){last=k;meta()}}
var ps=history.pushState,rs=history.replaceState;
history.pushState=function(){var r=ps.apply(this,arguments);tick();return r};
history.replaceState=function(){var r=rs.apply(this,arguments);tick();return r};
addEventListener('popstate',tick);addEventListener('hashchange',tick);addEventListener('load',tick);
document.addEventListener('DOMContentLoaded',function(){tick();var t=document.querySelector('title');if(t)new MutationObserver(tick).observe(t,{childList:true,characterData:true,subtree:true})});
window.open=function(u){if(u){try{post({a:'open',u:new URL(String(u),location.href).href})}catch(e){}}return null};
document.addEventListener('click',function(e){if(e.defaultPrevented||e.button!==0)return;var a=e.target&&e.target.closest&&e.target.closest('a[href]');
 if(a&&(a.target==='_blank'||e.ctrlKey||e.shiftKey)&&/^https?:/i.test(a.href)){e.preventDefault();post({a:'open',u:a.href})}},true);
addEventListener('keydown',function(e){
 if(e.ctrlKey&&!e.altKey){var k=e.key.toLowerCase();
  if(k==='t'||k==='w'||k==='l'||k==='r'||e.key==='Tab'){e.preventDefault();post({a:'key',k:e.key==='Tab'?(e.shiftKey?'shift-tab':'tab'):k})}}
 else if(e.key==='F5'){e.preventDefault();post({a:'key',k:'r'})}
 else if(e.altKey&&e.key==='ArrowLeft'){post({a:'key',k:'back'})}
 else if(e.altKey&&e.key==='ArrowRight'){post({a:'key',k:'fwd'})}
},true);
tick();})();`

const newTabHTML = `<!doctype html><html><head><meta charset="utf-8"><title>New tab</title><style>` + baseCSS + `
body{display:grid;place-items:center;-webkit-user-select:text;user-select:text}
main{width:min(560px,90vw);text-align:center}
h1{font-size:34px;font-weight:600;letter-spacing:-.02em;margin:0 0 6px}
p{color:var(--muted);margin:0 0 24px}
input{width:100%;height:48px;border:1px solid var(--line);border-radius:24px;padding:0 20px;background:var(--card);color:var(--fg);font:16px inherit}
input:focus{border-color:var(--accent);outline:0}
</style></head><body><main><h1>Fasty</h1><p>Less noise. More web.</p>
<input id="q" type="text" autofocus spellcheck="false" placeholder="Search or enter an address" aria-label="Search or enter an address"></main>
<script>
document.getElementById('q').addEventListener('keydown',function(e){if(e.key==='Enter'&&this.value.trim()){window.chrome.webview.postMessage(JSON.stringify({a:'nav',v:this.value}))}});
</script></body></html>`

// settingsHTML renders the settings page with the current values filled in.
func settingsHTML(s Settings) string {
	cur, _ := json.Marshal(s)
	var opts strings.Builder
	for _, e := range []struct{ k, n string }{
		{"duckduckgo", "DuckDuckGo"}, {"brave", "Brave Search"}, {"startpage", "Startpage"},
		{"google", "Google"}, {"bing", "Bing"},
	} {
		sel := ""
		if e.k == s.SearchEngine {
			sel = " selected"
		}
		opts.WriteString(`<option value="` + html.EscapeString(e.k) + `"` + sel + `>` + html.EscapeString(e.n) + `</option>`)
	}
	return `<!doctype html><html><head><meta charset="utf-8"><title>Settings</title><style>` + baseCSS + `
body{overflow:auto;-webkit-user-select:text;user-select:text}
main{max-width:640px;margin:0 auto;padding:40px 24px 80px}
h1{font-size:28px;font-weight:600;letter-spacing:-.02em;margin:0 0 4px}
.lead{color:var(--muted);margin:0 0 28px}
fieldset{border:1px solid var(--line);border-radius:20px;background:var(--card);margin:0 0 16px;padding:8px 20px}
.row{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:14px 0;border-bottom:1px solid var(--line)}
.row:last-child{border-bottom:0}
.row small{display:block;color:var(--muted)}
input[type=text],input[type=number],select{height:36px;border:1px solid var(--line);border-radius:12px;padding:0 12px;background:var(--bg);color:var(--fg);font:inherit;min-width:200px}
input[type=checkbox]{width:20px;height:20px;accent-color:var(--accent)}
#save{height:42px;padding:0 24px;border-radius:21px;background:var(--accent);color:var(--bg);font-weight:600}
#msg{margin-left:12px;color:var(--muted)}
</style></head><body><main>
<h1>Settings</h1><p class="lead">Saved on this computer only. There is no account and no sync.</p>
<fieldset><legend class="sr">General</legend>
<label class="row"><span>Home page<small>Leave empty for the new tab page</small></span><input type="text" id="homepage" placeholder="https://example.com"></label>
<label class="row"><span>Search engine<small>Used for anything typed that isn't an address</small></span><select id="engine">` + opts.String() + `</select></label>
<label class="row"><span>Restore tabs on start<small>Reopen the tabs you had open</small></span><input type="checkbox" id="restore"></label>
</fieldset>
<fieldset>
<label class="row"><span>Shield<small>Blocks requests to known ad and tracker hosts</small></span><input type="checkbox" id="shield"></label>
<label class="row"><span>Extended list<small>Also use the StevenBlack hosts list, downloaded once a week from GitHub</small></span><input type="checkbox" id="ext"></label>
</fieldset>
<fieldset>
<label class="row"><span>Sleep unused tabs after (minutes)<small>0 turns sleeping tabs off. A sleeping tab frees its page and reloads when you return</small></span><input type="number" id="sleep" min="0" max="1440"></label>
</fieldset>
<p><button id="save">Save</button><span id="msg" role="status"></span></p>
</main><script>
const cur=` + string(cur) + `;
const $=id=>document.getElementById(id);
$('homepage').value=cur.homepage;$('restore').checked=cur.restoreSession;$('shield').checked=cur.shield;$('ext').checked=cur.extendedShield;$('sleep').value=cur.sleepMinutes;
$('save').onclick=()=>{
  window.chrome.webview.postMessage(JSON.stringify({a:'save',s:{homepage:$('homepage').value.trim(),searchEngine:$('engine').value,shield:$('shield').checked,extendedShield:$('ext').checked,sleepMinutes:parseInt($('sleep').value||'0',10),restoreSession:$('restore').checked}}));
  $('msg').textContent='Saved.';setTimeout(()=>$('msg').textContent='',2500);
};
</script></body></html>`
}
