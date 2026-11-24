// Mouse-wheel scrolling that glides. WebKitGTK animates each wheel click on
// its own: about 100 ms, starting slowly and stopping dead before the next
// click, so turning the wheel feels like move-stop-move. Here each click adds
// to where the page is heading, and the page eases there quickly at first,
// then slowly (ease-out), so clicks in a row flow into one motion.
//
// Only whole wheel clicks are taken (wheelDelta a multiple of 120). Touchpads,
// Ctrl+wheel zoom, high-resolution wheels, and anything a page handles itself
// (maps, editors, games: they call preventDefault) are left to WebKit.
(() => {
  if (window.__fastyWheel) return;
  window.__fastyWheel = true;

  const TAU = 75;       // ms: how quickly the page catches up with the target
  const DONE = 0.5;     // px: close enough, stop
  const states = new Map(); // scroller -> {x, y, tx, ty, wx, wy}
  let frame = 0, last = 0;

  const root = () => document.scrollingElement || document.documentElement;

  function scrollable(el, axis) {
    if (el === root()) return true;
    const s = getComputedStyle(el);
    const o = axis === 'y' ? s.overflowY : s.overflowX;
    if (o !== 'auto' && o !== 'scroll' && o !== 'overlay') return false;
    return axis === 'y' ? el.scrollHeight > el.clientHeight : el.scrollWidth > el.clientWidth;
  }

  function snaps(el) {
    const s = getComputedStyle(el === root() ? document.documentElement : el);
    return s.scrollSnapType && s.scrollSnapType !== 'none';
  }

  function limit(el, axis) {
    return axis === 'y' ? el.scrollHeight - el.clientHeight : el.scrollWidth - el.clientWidth;
  }

  // where the scroller is heading, or where it is if it isn't moving
  function heading(el, axis) {
    const st = states.get(el);
    if (st) return axis === 'y' ? st.ty : st.tx;
    return axis === 'y' ? el.scrollTop : el.scrollLeft;
  }

  // The innermost element under the pointer that can still move this way.
  // Returns null to leave the event to WebKit, false to swallow it.
  function target(e, axis, delta) {
    const path = e.composedPath();
    for (const n of path) {
      if (!(n instanceof Element)) continue;
      if (n === document.body && root() !== document.body) continue;
      if (!scrollable(n, axis)) continue;
      const at = heading(n, axis), max = limit(n, axis);
      if ((delta > 0 && at < max - 1) || (delta < 0 && at > 1)) return snaps(n) ? null : n;
      const ob = getComputedStyle(n)[axis === 'y' ? 'overscrollBehaviorY' : 'overscrollBehaviorX'];
      if (ob && ob !== 'auto' && n !== root()) return false;
    }
    const r = root();
    if (!path.includes(r) && scrollable(r, axis)) {
      const at = heading(r, axis), max = limit(r, axis);
      if ((delta > 0 && at < max - 1) || (delta < 0 && at > 1)) return snaps(r) ? null : r;
    }
    return null; // nothing here can move: let WebKit pass it to the parent frame
  }

  function write(el, st) {
    el.scrollTo({ left: st.x, top: st.y, behavior: 'instant' });
    st.wx = el.scrollLeft;
    st.wy = el.scrollTop;
  }

  function tick(now) {
    const dt = Math.min(now - last, 50);
    last = now;
    const k = 1 - Math.exp(-dt / TAU);
    for (const [el, st] of states) {
      // Someone else moved it (scrollbar, keys, the page): stop and let go.
      if (!el.isConnected || Math.abs(el.scrollTop - st.wy) > 2 || Math.abs(el.scrollLeft - st.wx) > 2) {
        states.delete(el);
        continue;
      }
      st.x += (st.tx - st.x) * k;
      st.y += (st.ty - st.y) * k;
      if (Math.abs(st.tx - st.x) < DONE && Math.abs(st.ty - st.y) < DONE) {
        st.x = st.tx;
        st.y = st.ty;
        write(el, st);
        states.delete(el);
        continue;
      }
      write(el, st);
    }
    frame = states.size ? requestAnimationFrame(tick) : 0;
  }

  addEventListener('wheel', (e) => {
    if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey || e.deltaMode !== 0) return;
    const wx = e.wheelDeltaX || 0, wy = e.wheelDeltaY || 0;
    // whole wheel clicks only, one axis at a time
    if ((wx === 0) === (wy === 0)) return;
    if ((wx || wy) % 120 !== 0) return;
    const axis = wy ? 'y' : 'x';
    const delta = axis === 'y' ? e.deltaY : e.deltaX;
    if (!delta) return;
    const el = target(e, axis, delta);
    if (el === null) return;
    e.preventDefault();
    if (el === false) return;
    let st = states.get(el);
    if (!st) {
      st = { x: el.scrollLeft, y: el.scrollTop, wx: el.scrollLeft, wy: el.scrollTop };
      st.tx = st.x;
      st.ty = st.y;
      states.set(el, st);
    }
    if (axis === 'y') st.ty = Math.max(0, Math.min(limit(el, 'y'), st.ty + delta));
    else st.tx = Math.max(0, Math.min(limit(el, 'x'), st.tx + delta));
    if (!frame) {
      last = performance.now();
      frame = requestAnimationFrame(tick);
    }
  }, { passive: false });
})();
