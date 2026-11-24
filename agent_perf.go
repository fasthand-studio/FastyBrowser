package main

import (
	"context"
	"encoding/json"

	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
)

// clearCache drops the HTTP disk and memory caches of the main session.
// Cookies, logins and site storage stay.
func clearCache(b *Browser, done func(error)) {
	mgr := b.app.engine.session.WebsiteDataManager()
	mgr.Clear(context.Background(), webkit.WebsiteDataDiskCache|webkit.WebsiteDataMemoryCache, 0,
		func(res gio.AsyncResulter) { done(mgr.ClearFinish(res)) })
}

// agentPerf measures a page load. With a url it opens a private tab (its own
// empty cache, so the load is cold), waits for it to finish and settle,
// measures, closes it. Without a url it measures the current tab as it is.
// With cold=false and a url, the page loads in a normal tab and may use the
// cache. fresh=true clears the main cache first (cold=false only).
func agentPerf(b *Browser, cmd agentCmd, ch chan<- agentReply) {
	measure := func(t *Tab, after func()) {
		t.run(perfScript, func(result string) {
			if after != nil {
				after()
			}
			ch <- agentReply{OK: true, Result: json.RawMessage(result)}
		})
	}
	if cmd.URL == "" {
		t := agentTab(b)
		if t == nil || t.view == nil {
			ch <- agentReply{Error: "no page"}
			return
		}
		measure(t, nil)
		return
	}
	prev := b.current
	start := func(private bool) {
		t := b.openTab(cmd.URL, true, nil, private, "", false)
		waited := 0
		var poll func() bool
		poll = func() bool {
			waited += 250
			if t.loading && waited < 30000 {
				return true
			}
			// let late fetches, lazy images and paint metrics land
			coreglib.TimeoutAdd(2500, func() bool {
				measure(t, func() {
					b.closeTab(t)
					if prev != nil {
						b.selectTab(prev)
					}
				})
				return false
			})
			return false
		}
		coreglib.TimeoutAdd(500, poll)
	}
	if cmd.Private || !cmd.Warm {
		start(true)
		return
	}
	if cmd.Clear {
		clearCache(b, func(error) { start(false) })
		return
	}
	start(false)
}

// perfScript runs inside the page and returns JSON: timings, the slowest
// resources, and plain-language findings.
const perfScript = `(()=>{
const n=performance.getEntriesByType('navigation')[0]||{};
const r=performance.getEntriesByType('resource');
const fcp=(performance.getEntriesByName('first-contentful-paint')[0]||{}).startTime||0;
let lcp=0,lcpEl='',cls=0;try{const l=performance.getEntriesByType('largest-contentful-paint');const e=l[l.length-1];if(e){lcp=e.startTime;lcpEl=(e.url||'').slice(0,100)}}catch(e){}
try{performance.getEntriesByType('layout-shift').forEach(e=>{if(!e.hadRecentInput)cls+=e.value})}catch(e){}
const R=x=>Math.round(x);
const host=location.host;
const rows=r.map(x=>({url:x.name.replace(/^https?:\/\//,'').slice(0,110),type:x.initiatorType,start:R(x.startTime),ms:R(x.duration),wait:R(x.responseStart-x.requestStart),size:x.transferSize,body:x.encodedBodySize,third:new URL(x.name).host!==host,end:R(x.responseEnd)}));
const sum=a=>a.reduce((s,x)=>s+x,0);
const byType={};rows.forEach(x=>{const k=x.type;byType[k]=byType[k]||{n:0,bytes:0};byType[k].n++;byType[k].bytes+=x.body||0});
const f=[];
const ttfb=n.responseStart||0;
if(ttfb>600)f.push('Slow server response: first byte took '+R(ttfb)+' ms. Check the origin, add CDN/edge caching of the HTML.');
if(fcp>1800)f.push('First contentful paint is '+R(fcp)+' ms (good is under 1800). Reduce render-blocking CSS/JS.');
if(lcp&&lcp>2500)f.push('Largest contentful paint is '+R(lcp)+' ms (good is under 2500)'+(lcpEl?': '+lcpEl:'')+'. Preload and compress that element.');
if(cls>0.1)f.push('Layout shift score is '+cls.toFixed(3)+' (good is under 0.1). Reserve sizes for images and banners.');
const api=rows.filter(x=>(x.type==='fetch'||x.type==='xmlhttprequest'));
const slowApi=api.filter(x=>x.ms>300);
if(slowApi.length)f.push(slowApi.length+' slow API call(s) over 300 ms: '+slowApi.map(x=>x.url.split('?')[0]+' ('+x.ms+' ms)').join(', ')+'. Cache them at the edge or fetch on the server.');
const dup={};api.forEach(x=>{const k=x.url;dup[k]=(dup[k]||0)+1});
const dups=Object.keys(dup).filter(k=>dup[k]>1);
if(dups.length)f.push('Same request made more than once: '+dups.map(k=>k+' x'+dup[k]).join(', ')+'. Deduplicate.');
const late=api.filter(x=>x.start>(fcp||0)&&x.start>400);
if(late.length>2)f.push(late.length+' API calls start after '+R(Math.min(...late.map(x=>x.start)))+' ms, after the JS has loaded. Data fetched on the client delays content; inline it in the HTML or start it earlier.');
const bigImg=rows.filter(x=>(x.type==='img'||x.type==='css'&&0)&&x.body>150000);
if(bigImg.length)f.push('Large images over 150 KB: '+bigImg.map(x=>x.url+' ('+R(x.body/1024)+' KB)').join(', ')+'. Resize or use AVIF/WebP.');
const bigJs=rows.filter(x=>x.type==='script'&&x.body>150000);
if(bigJs.length)f.push('Large scripts over 150 KB: '+bigJs.map(x=>x.url+' ('+R(x.body/1024)+' KB)').join(', ')+'. Split or lazy-load.');
const jsBytes=sum(rows.filter(x=>x.type==='script').map(x=>x.body||0));
if(jsBytes>500000)f.push('JavaScript totals '+R(jsBytes/1024)+' KB. Trim unused code.');
const third=rows.filter(x=>x.third&&!/fasthand/.test(x.url));
if(third.length>8)f.push(third.length+' third-party requests. Defer or remove ones that are not needed.');
if(rows.length>80)f.push(rows.length+' requests on load. Bundle or lazy-load below-the-fold assets.');
const noTime=rows.filter(x=>x.third&&x.size===0&&x.body===0).length;
if(!f.length)f.push('No major problems found.');
return JSON.stringify({url:location.href,timing_ms:{dns:R(n.domainLookupEnd-n.domainLookupStart),connect:R(n.connectEnd-n.connectStart),ttfb:R(ttfb),download:R(n.responseEnd-n.responseStart),dom_interactive:R(n.domInteractive),dom_content_loaded:R(n.domContentLoadedEventEnd),load:R(n.loadEventEnd),fcp:R(fcp),lcp:R(lcp),last_resource_end:R(Math.max(0,...rows.map(x=>x.end)))},cls:+cls.toFixed(3),requests:rows.length,transfer_kb:R(sum(rows.map(x=>x.size))/1024),encoded_kb:R(sum(rows.map(x=>x.body||0))/1024),html_kb:R((n.encodedBodySize||0)/1024),by_type:byType,slowest:rows.slice().sort((a,b)=>b.ms-a.ms).slice(0,10).map(x=>({url:x.url,type:x.type,start:x.start,ms:x.ms,kb:R((x.body||0)/1024)})),findings:f,note:'Cross-origin resources hide their sizes unless the server sends Timing-Allow-Origin'})
})()`
