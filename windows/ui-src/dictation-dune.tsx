"use client"
// Dictation Dune by Louis Studios.
// 330 more hand-made React components at https://studios.lou15.com/?ref=21st-code

import * as React from "react"

/* Dictation Dune (Louis Studios)
   A dictation input whose voice level is a mound of grains at the bottom edge. Every recognised
   word is carried out of the mound by a stream of grains that lands on the word's own letters,
   which then sharpen from a smear into text. One file, React only, drawn on canvas. */

export type DictationDuneLook = "glyphs" | "dots"

export interface DictationDuneHandle {
  /** Start listening (same as pressing the mic button). */
  start: () => void
  /** Stop listening (same as pressing the stop button). */
  stop: () => void
  /** Append one recognised word; a stream of grains carries it into the transcript. */
  pushWord: (word: string) => void
  /** Empty the transcript with the blur wipe. */
  clear: () => void
}

export interface DictationDuneProps {
  /** What the mound is built from: "glyphs" (small text characters) or "dots" (a grid of round dots). */
  look?: DictationDuneLook
  /** Controlled transcript text. Words you append fly in from the mound; pair it with onTranscript. */
  value?: string
  /** Starting transcript when the text is uncontrolled. */
  defaultValue?: string
  /** Streaming alternative to value: the recognised words so far. Every word you add to the end triggers a grain flight into that word. */
  words?: string[]
  /** Controlled listening state. Leave it unset and the mic button toggles it by itself. Defaults to false. */
  listening?: boolean
  /** Whether the control starts out listening when listening is uncontrolled. Defaults to false. */
  defaultListening?: boolean
  /** Controlled finishing state shown after a stop (three dots on the button). Leave it unset for a short automatic one. Defaults to false. */
  processing?: boolean
  /** Live input level from 0 to 1 (for example from an AnalyserNode). The mound heaves with it. */
  level?: number
  /** Called when the user asks to start (button, Space or Enter). */
  onStart?: () => void
  /** Called with the current transcript when the user asks to stop (button, Space, Enter or Escape). */
  onStop?: (text: string) => void
  /** Called with the full transcript every time a word is added or the text is cleared. */
  onTranscript?: (text: string) => void
  /** Called when the built-in microphone or speech recognition fails or is refused. */
  onError?: (error: Error) => void
  /** Turn on the built-in microphone: getUserMedia drives the level and the Web Speech API fills the transcript. The browser only asks for the microphone after the user presses start, and where speech recognition is missing only the level runs. Defaults to false. */
  microphone?: boolean
  /** Language tag for the built-in speech recognition. */
  lang?: string
  /** Play a simulated dictation of demoText in a loop, with a simulated voice level. Defaults to false. */
  demo?: boolean
  /** The sentence the demo dictates. */
  demoText?: string
  /** Empty the transcript each time listening starts. Defaults to false. */
  clearOnStart?: boolean
  /** Turn the mic button off. Defaults to false. */
  disabled?: boolean
  /** Hint shown while the transcript is empty. */
  placeholder?: string
  /** Hint shown while listening, before the first word lands. */
  listeningLabel?: string
  /** Status word shown next to the dot while the control finishes up. */
  processingLabel?: string
  /** Name of the whole control for assistive technology. */
  ariaLabel?: string
  /** Characters the glyph mound is set in, ordered from lightest to heaviest. */
  glyphs?: string
  /** Fixed scale of the whole control (1 means 16 px type). Leave it unset to fit the container. */
  size?: number
  /** Text, grain and icon colour. */
  color?: string
  /** Colour of the hint and status text. */
  muted?: string
  /** Fill of the input box. */
  surface?: string
  /** Hairline around the input box. */
  border?: string
  /** Fill of the round mic button. */
  buttonColor?: string
  /** Backdrop behind the box. */
  background?: string
  /** Font stack for the transcript and labels. */
  font?: string
  /** Class for the root element. */
  className?: string
  /** Inline style for the root element. */
  style?: React.CSSProperties
}

const RAMP = "/|()1?jtfrxnuvczYUJCLQ0OZmwqpdbkhao*#MW&8%B@$"
const DUST = ";:+~=-'"
const MONO = 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Monaco, Consolas, monospace'
const BASE_W = 440
const BASE_H = 136
const MAX_GRAINS = 440
const GRAINS_PER_WORD = 110

type Item = { id: number; text: string; fly: boolean; order: number }
type Grain = { sx: number; sy: number; cx: number; cy: number; tx: number; ty: number; t0: number; dur: number; jx: number; jy: number; w: number }
type WordAnim = { id: number; el: HTMLElement; born: number; grains: Grain[] }
type Geo = { s: number; w: number; h: number }

const clamp01 = (v: number) => (v < 0 ? 0 : v > 1 ? 1 : v)
const smooth = (a: number, b: number, v: number) => { const t = clamp01((v - a) / (b - a)); return t * t * (3 - 2 * t) }

// Small seeded value noise: enough to make the mound shimmer, cheap enough for every cell.
const NOISE = (() => { const t = new Float32Array(512); let s = 1234567; for (let i = 0; i < 512; i++) { s = (s * 1664525 + 1013904223) >>> 0; t[i] = s / 4294967296 } return t })()
const nh = (i: number, j: number, k: number) => NOISE[(i * 73 + j * 151 + k * 211) & 511]
function noise3(x: number, y: number, z: number) {
  const xi = Math.floor(x), yi = Math.floor(y), zi = Math.floor(z)
  let fx = x - xi, fy = y - yi, fz = z - zi
  fx = fx * fx * (3 - 2 * fx); fy = fy * fy * (3 - 2 * fy); fz = fz * fz * (3 - 2 * fz)
  const a = nh(xi, yi, zi) + (nh(xi + 1, yi, zi) - nh(xi, yi, zi)) * fx
  const b = nh(xi, yi + 1, zi) + (nh(xi + 1, yi + 1, zi) - nh(xi, yi + 1, zi)) * fx
  const c = nh(xi, yi, zi + 1) + (nh(xi + 1, yi, zi + 1) - nh(xi, yi, zi + 1)) * fx
  const d = nh(xi, yi + 1, zi + 1) + (nh(xi + 1, yi + 1, zi + 1) - nh(xi, yi + 1, zi + 1)) * fx
  const e = a + (b - a) * fy, f = c + (d - c) * fy
  return e + (f - e) * fz
}

function useLatest<T>(v: T) { const r = React.useRef(v); r.current = v; return r }
const useIsoLayoutEffect = typeof window !== "undefined" ? React.useLayoutEffect : React.useEffect

function planDemo(text: string) {
  const toks = text.split(/\s+/).filter(Boolean)
  const plan: { w: string; at: number }[] = []
  let at = 1.5
  for (const w of toks) { plan.push({ w, at }); at += 0.2 + 0.035 * Math.min(10, w.length) + (/[.!?]$/.test(w) ? 0.25 : 0) }
  return plan
}

export const DictationDune = React.forwardRef<DictationDuneHandle, DictationDuneProps>(function DictationDune(
  {
    look = "glyphs",
    value,
    defaultValue = "",
    words,
    listening,
    defaultListening = false,
    processing,
    level,
    onStart,
    onStop,
    onTranscript,
    onError,
    microphone = false,
    lang = "en-US",
    demo = false,
    demoText = "Pack the tent and the blue kettle tonight. Tomorrow we climb the ridge before sunrise.",
    clearOnStart = false,
    disabled = false,
    placeholder = "Press the mic and start talking",
    listeningLabel = "Go ahead...",
    processingLabel = "tidying",
    ariaLabel = "Dictation",
    glyphs = RAMP,
    size,
    color = "#ededed",
    muted = "#7d7d83",
    surface = "#1e1e1e",
    border = "#292929",
    buttonColor = "#343434",
    background = "transparent",
    font = '-apple-system, BlinkMacSystemFont, "SF Pro Text", "Segoe UI", system-ui, sans-serif',
    className,
    style,
  },
  ref
) {
  const uid = React.useId().replace(/[^a-zA-Z0-9]/g, "")
  const [inner, setInner] = React.useState(defaultValue)
  const [lisInner, setLisInner] = React.useState(defaultListening)
  const [procInner, setProcInner] = React.useState(false)
  const [secs, setSecs] = React.useState(0)
  const [geo, setGeo] = React.useState({ s: 1, w: BASE_W, h: BASE_H } as Geo)
  const [, bump] = React.useReducer((n: number) => n + 1, 0)

  const controlledText = words !== undefined || value !== undefined
  const text = words !== undefined ? words.join(" ") : value !== undefined ? value : inner
  const isListening = listening !== undefined ? listening : lisInner
  const isProcessing = !isListening && (processing !== undefined ? processing : procInner)

  const rootRef = React.useRef(null as HTMLDivElement | null)
  const boxRef = React.useRef(null as HTMLDivElement | null)
  const cvRef = React.useRef(null as HTMLCanvasElement | null)
  const glowRef = React.useRef(null as HTMLCanvasElement | null)
  const btnRef = React.useRef(null as HTMLButtonElement | null)
  const recDotRef = React.useRef(null as HTMLSpanElement | null)
  const procDotRef = React.useRef(null as HTMLSpanElement | null)
  const thinkRef = React.useRef(null as HTMLSpanElement | null)
  const wordEls = React.useRef(new Map<number, HTMLElement>())
  const textRef = React.useRef(text)
  textRef.current = text

  const P = useLatest({ look, level, onStart, onStop, onTranscript, onError, microphone, lang, demo, demoText, clearOnStart, disabled, glyphs, color, controlledText, isListening, isProcessing, listeningControlled: listening !== undefined, processingControlled: processing !== undefined, geo, size })

  // ---- transcript bookkeeping: which words are new (and fly), which are leaving (and wipe) ----
  const rec = React.useRef({ text: null as string | null, items: [] as Item[], leaving: null as Item[] | null, nextId: 1, mounted: false })
  if (rec.current.text !== text) {
    const r = rec.current
    const toks = text.split(/\s+/).filter(Boolean)
    const prev = r.items
    let k = 0
    while (k < prev.length && k < toks.length && prev[k].text === toks[k]) k++
    const items = prev.slice(0, k)
    for (let i = k; i < toks.length; i++) items.push({ id: r.nextId++, text: toks[i], fly: r.mounted && toks.length - i <= 8, order: i - k })
    r.leaving = !toks.length && prev.length && r.mounted ? prev : null
    r.items = items
    r.text = text
  }
  const items = rec.current.items
  const leaving = rec.current.leaving
  const shown = leaving || items

  // ---- actions shared by the button, the keyboard, the handle, the demo and the microphone ----
  const api = React.useRef({
    setAll(next: string) {
      if (next === textRef.current) return
      textRef.current = next
      if (!P.current.controlledText) setInner(next)
      P.current.onTranscript?.(next)
    },
    append(word: string) {
      const w = word.trim()
      if (!w) return
      api.current.setAll(textRef.current ? textRef.current + " " + w : w)
    },
    start(silent?: boolean) {
      const p = P.current
      if (p.disabled || p.isListening) return
      if (p.clearOnStart) api.current.setAll("")
      if (!silent) p.onStart?.()
      if (!p.listeningControlled) { setProcInner(false); setLisInner(true) }
    },
    stop(silent?: boolean) {
      const p = P.current
      if (!p.isListening) return
      if (!silent) p.onStop?.(textRef.current)
      if (!p.listeningControlled) { setLisInner(false); if (!p.processingControlled) setProcInner(true) }
    },
    toggle() { if (P.current.isListening) api.current.stop(); else api.current.start() },
  })

  React.useImperativeHandle(ref, () => ({
    start: () => api.current.start(),
    stop: () => api.current.stop(),
    pushWord: (w: string) => api.current.append(w),
    clear: () => api.current.setAll(""),
  }), [])

  // ---- the engine: one clock for the mound, the grains, the words and the button ----
  const eng = React.useRef(null as any)

  useIsoLayoutEffect(() => {
    const root = rootRef.current!, box = boxRef.current!, cv = cvRef.current!, glow = glowRef.current!
    const ctx = cv.getContext("2d")!
    const gctx = glow.getContext("2d")!
    const samp = document.createElement("canvas")
    const sctx = samp.getContext("2d", { willReadFrequently: true } as any) as CanvasRenderingContext2D
    const mq = window.matchMedia ? window.matchMedia("(prefers-reduced-motion: reduce)") : null
    const E = {
      T: 0, last: 0, raf: 0, onScreen: true, reduced: !!(mq && mq.matches), dead: false,
      rise: 0, floorW: 0, L: 0, Lv: 0, spout: 0, lean: 0,
      bs: 1, bv: 0, pressed: false, pressUntil: -1,
      px: -1, hover: 0, hoverT: 0,
      grains: 0, anims: [] as WordAnim[], seen: new Set<number>(),
      wipe: null as null | { t0: number; els: HTMLElement[] },
      listening: false, listenAt: 0, secs: 0, procEnd: -1, processing: false,
      demo: { state: "rest", nextStart: 0.5, speakAt: 0, stopRel: 0, stopT: 0, idx: 0, plan: [] as { w: string; at: number }[], pressed: false, asked: false, scripted: false, cleared: false },
      mic: null as any, micLevel: 0,
      dpr: 1, W: BASE_W, H: BASE_H, s: 1,
      atlas: null as HTMLCanvasElement | null, atlasKey: "", acw: 0, ach: 0,
      rgb: [237, 237, 237], rgbKey: "",
      gw: 0, gh: 0, gimg: null as ImageData | null, glowDirty: false,
    }
    eng.current = E
    let seed = 99
    const rnd = () => { seed = (seed * 1664525 + 1013904223) >>> 0; return seed / 4294967296 }
    const gauss = () => (rnd() + rnd() + rnd() - 1.5) / 1.5

    // -- geometry --
    function measure() {
      const cw = root.clientWidth, chh = root.clientHeight
      const gut = cw < 480 ? 12 : 24
      const fixed = P.current.size
      let s: number, w: number, h: number
      const fit = Math.min((cw - 2 * gut) / BASE_W, (chh - 2 * gut) / BASE_H)
      if (fixed === undefined && fit >= 1) { s = Math.min(fit, 1.8); w = BASE_W * s; h = BASE_H * s }
      else {
        s = fixed !== undefined ? fixed : Math.max(0.6, Math.min(1, (chh - 2 * gut) / 150))
        w = Math.max(200 * s, Math.min(BASE_W * s, cw - 2 * gut))
        h = (w < 400 * s ? 150 : BASE_H) * s
      }
      w = Math.round(w); h = Math.round(h)
      const g = P.current.geo
      if (Math.abs(g.s - s) > 0.001 || g.w !== w || g.h !== h) setGeo({ s, w, h })
      sizeCanvas(s, w, h)
    }
    function sizeCanvas(s: number, w: number, h: number) {
      const dpr = Math.min(window.devicePixelRatio || 1, 2.5)
      if (E.s === s && E.W === w && E.H === h && E.dpr === dpr && cv.width) return
      E.s = s; E.W = w; E.H = h; E.dpr = dpr
      cv.width = Math.round(w * dpr); cv.height = Math.round(h * dpr)
      E.gw = 0
      kick()
    }

    function colorRgb(c: string) {
      if (E.rgbKey === c) return E.rgb
      samp.width = 2; samp.height = 2
      sctx.setTransform(1, 0, 0, 1, 0, 0)
      sctx.fillStyle = "#ffffff" // ls:keep
      sctx.fillStyle = c
      sctx.fillRect(0, 0, 2, 2)
      const d = sctx.getImageData(0, 0, 1, 1).data
      E.rgb = [d[0], d[1], d[2]]; E.rgbKey = c
      return E.rgb
    }

    function buildAtlas(chars: string, cw: number, ch: number, color: string) {
      const key = chars + "|" + cw.toFixed(3) + "|" + ch.toFixed(3) + "|" + color + "|" + E.dpr
      if (E.atlasKey === key && E.atlas) return
      const a = E.atlas || document.createElement("canvas")
      const pw = Math.ceil(cw * E.dpr) + 2, ph = Math.ceil(ch * E.dpr) + 2
      a.width = pw * chars.length; a.height = ph
      const c = a.getContext("2d")!
      c.clearRect(0, 0, a.width, a.height)
      c.fillStyle = color
      c.textAlign = "center"; c.textBaseline = "middle"
      c.font = `600 ${(ch * E.dpr * 0.98).toFixed(2)}px ${MONO}`
      for (let i = 0; i < chars.length; i++) c.fillText(chars[i], i * pw + pw / 2, ph / 2 + ph * 0.04)
      E.atlas = a; E.atlasKey = key; E.acw = pw; E.ach = ph
    }

    // -- the mound: one height profile, read by both looks --
    function moundParams() {
      const W = E.W, H = E.H
      const Hm = H * E.rise * (0.085 + 0.43 * clamp01(E.L))
      const Wm = W * (0.225 + 0.03 * clamp01(E.L))
      const cx = W / 2 + E.lean * W
      return { Hm, Wm, cx }
    }
    function heightAt(x: number, m: { Hm: number; Wm: number; cx: number }) {
      const u = (x - m.cx) / m.Wm
      const au = u < 0 ? -u : u
      const sp = Math.min(1, E.spout), tm = E.reduced ? 0 : E.T
      const tri = au < 1 ? Math.min(1, 1.09 * Math.pow(1 - au, 1 + 0.3 * sp)) : 0
      const wob = 1 + 0.26 * (noise3(x * 0.022 / E.s, tm * 1.25, 3.7) - 0.5)
      const lumpAt = 0.46 * Math.sin(tm * 0.83 + 1.3)
      const lump = 0.2 * Math.exp(-((u - lumpAt) * (u - lumpAt)) / 0.07) * (0.5 + 0.5 * Math.sin(tm * 1.9))
      const spt = 0.34 * sp * Math.exp(-(u * u) / 0.035)
      let h = m.Hm * (tri * wob + lump * (au < 1 ? 1 : 0) + spt)
      if (E.hover > 0.01 && E.px >= 0) { const d = (x - E.px) / (E.W * 0.05); h += m.Hm * 0.2 * E.hover * Math.exp(-d * d) * (au < 1.05 ? 1 : 0) }
      return h
    }

    function drawMound(p: typeof P.current) {
      const s = E.s, W = E.W, H = E.H
      const dots = p.look === "dots"
      const cw = (dots ? 5 : 5.08) * s, ch = (dots ? 5 : 8.5) * s
      const cols = Math.floor(W / cw / 2) * 2 + 1
      const rowsMax = Math.ceil((H * 0.74) / ch)
      const x0 = W / 2 - ((cols - 1) / 2) * cw
      const yb = H - (dots ? 3.7 : 5.6) * s
      if (E.gw !== cols || E.gh !== rowsMax) {
        E.gw = cols; E.gh = rowsMax
        glow.width = cols; glow.height = rowsMax
        glow.style.left = x0 - cw / 2 + "px"; glow.style.width = cols * cw + "px"
        glow.style.height = rowsMax * ch + "px"; glow.style.top = yb + ch / 2 - rowsMax * ch + "px"
        E.gimg = gctx.createImageData(cols, rowsMax)
      }
      const img = E.gimg!, gd = img.data
      if (E.rise < 0.004) {
        if (E.glowDirty) { gd.fill(0); gctx.putImageData(img, 0, 0); E.glowDirty = false }
        return
      }
      gd.fill(0)
      const rgb = colorRgb(p.color)
      const m = moundParams()
      const fade = E.listening || E.processing ? smooth(0, 0.8, E.rise) : smooth(0, 0.3, E.rise)
      const floorHalf = W * (dots ? 0.405 : 0.375) * E.floorW
      const tz = E.reduced ? 0 : E.T * 1.5
      const N = Math.max(2, p.glyphs.length)
      if (!dots) buildAtlas(p.glyphs + DUST, cw, ch, p.color)
      const atlas = E.atlas!, dpr = E.dpr, acw = E.acw, ach = E.ach
      const big: number[] = [], mid: number[] = [], tiny: number[] = []
      for (let i = 0; i < cols; i++) {
        const x = x0 + i * cw
        const hr = (heightAt(x, m) + (noise3(i * 0.9, tz * 1.4, 9.1) - 0.5) * ch * 0.7) / ch
        const fu = floorHalf > 1 ? Math.abs(x - W / 2) / floorHalf : 9
        const top = Math.min(rowsMax - 1, Math.ceil(hr) + 3)
        for (let j = 0; j <= top; j++) {
          let v = -1
          if (hr > j - 0.12 && (j > 0 || hr > 0.5)) v = clamp01((hr - j + 0.45) / 1.6)
          if (j === 0 && fu < 1) { const fd = (dots ? 0.07 : 0.84) * (1 - Math.pow(fu, 5)); if (fd > v) v = fd }
          const n = clamp01((noise3(i * 1.73, j * 1.37 + 20, tz) - 0.5) * 1.7 + 0.5)
          const y = yb - j * ch
          if (v < 0) {
            // loose grains just above the surface
            const over = j - hr
            if (j === 0 || over > 2.6 || hr < 0.55) continue
            if (dots) { if (n > 0.5 + over * 0.17) tiny.push(x, y, 0.3 * fade) }
            else if (nh(i, j, Math.floor(tz * 2.2)) > 0.972) {
              const di = N + ((i * 7 + j * 3) % DUST.length)
              ctx.globalAlpha = 0.32 * fade
              ctx.drawImage(atlas, di * acw, 0, acw, ach, x - acw / dpr / 2, y - ach / dpr / 2, acw / dpr, ach / dpr)
            }
            continue
          }
          let ga: number
          if (dots) {
            const share = 0.46 * Math.sqrt(v)
            if (n > 1 - share) { big.push(x, y); ga = 0.8 }
            else if (n > 0.88 - share) { mid.push(x, y); ga = 0.3 }
            else { tiny.push(x, y, (0.42 + 0.3 * v) * fade); ga = 0.05 }
          } else {
            const idx = Math.min(N - 1, Math.floor((v * 0.79 + n * 0.21) * N))
            ctx.globalAlpha = (0.24 + 0.76 * Math.pow(v, 1.35)) * fade
            ctx.drawImage(atlas, idx * acw, 0, acw, ach, x - acw / dpr / 2, y - ach / dpr / 2, acw / dpr, ach / dpr)
            ga = v * v * (0.55 + 0.45 * n)
          }
          const row = rowsMax - 1 - j
          if (row >= 0) { const o = (row * cols + i) * 4; gd[o] = rgb[0]; gd[o + 1] = rgb[1]; gd[o + 2] = rgb[2]; gd[o + 3] = Math.round(255 * ga * fade) }
        }
      }
      if (dots) {
        ctx.fillStyle = p.color
        const TAU = Math.PI * 2
        ctx.globalAlpha = fade
        ctx.beginPath()
        for (let k = 0; k < big.length; k += 2) { ctx.moveTo(big[k] + cw * 0.38, big[k + 1]); ctx.arc(big[k], big[k + 1], cw * 0.38, 0, TAU) }
        ctx.fill()
        ctx.globalAlpha = 0.82 * fade
        ctx.beginPath()
        for (let k = 0; k < mid.length; k += 2) { ctx.moveTo(mid[k] + cw * 0.25, mid[k + 1]); ctx.arc(mid[k], mid[k + 1], cw * 0.25, 0, TAU) }
        ctx.fill()
        const tr = cw * 0.12
        for (let k = 0; k < tiny.length; k += 3) { ctx.globalAlpha = tiny[k + 2]; ctx.fillRect(tiny[k] - tr, tiny[k + 1] - tr, tr * 2, tr * 2) }
      }
      ctx.globalAlpha = 1
      gctx.putImageData(img, 0, 0)
      E.glowDirty = true
    }

    // -- grains --
    function launch(it: Item, el: HTMLElement) {
      const s = E.s
      const br = box.getBoundingClientRect(), r = el.getBoundingClientRect()
      if (!r.width || !br.width) { el.style.opacity = ""; return }
      const left = r.left - br.left, top = r.top - br.top
      const cs = getComputedStyle(el)
      const fpx = parseFloat(cs.fontSize) || 16 * s
      const g = 2.15 * s
      const sw = Math.ceil(r.width / g) + 2, sh = Math.ceil(r.height / g) + 2
      samp.width = sw; samp.height = sh
      sctx.setTransform(1 / g, 0, 0, 1 / g, 1, 1)
      sctx.font = `${cs.fontWeight} ${fpx}px ${cs.fontFamily}`
      const mt = sctx.measureText(it.text) as any
      const asc = mt.fontBoundingBoxAscent || fpx * 0.95, desc = mt.fontBoundingBoxDescent || fpx * 0.24
      sctx.textBaseline = "alphabetic"
      sctx.fillStyle = "#000000" // ls:keep
      sctx.fillText(it.text, 0, (r.height - (asc + desc)) / 2 + asc)
      const data = sctx.getImageData(0, 0, sw, sh).data
      let pts: number[] = []
      for (let py = 0; py < sh; py++) for (let px = 0; px < sw; px++) if (data[(py * sw + px) * 4 + 3] > 78) pts.push(left + (px - 0.5) * g, top + (py - 0.5) * g)
      const room = Math.max(12, Math.min(GRAINS_PER_WORD, MAX_GRAINS - E.grains))
      if (pts.length / 2 > room) {
        const keep: number[] = [], n = pts.length / 2
        for (let k = 0; k < room; k++) { const q = Math.floor(((k + rnd()) / room) * n); keep.push(pts[q * 2], pts[q * 2 + 1]) }
        pts = keep
      }
      const born = E.T + it.order * 0.09
      const m = moundParams()
      const grains: Grain[] = []
      const wcx = left + r.width / 2
      for (let k = 0; k < pts.length; k += 2) {
        const tx = pts[k], ty = pts[k + 1]
        const sx = m.cx + gauss() * E.W * 0.06
        const hh = Math.max(3 * s, heightAt(sx, m))
        const sy = E.H - 4 * s - hh * (0.5 + 0.5 * rnd())
        const lead = (tx - left) / Math.max(1, r.width)
        const dist = Math.hypot(tx - sx, ty - sy)
        grains.push({
          sx, sy, tx, ty,
          cx: sx + (tx - sx) * (0.2 + 0.24 * rnd()) + gauss() * 5 * s,
          cy: ty + (sy - ty) * (0.02 + 0.12 * rnd()) + gauss() * 5 * s,
          t0: born + lead * 0.12 + rnd() * 0.26,
          dur: 0.21 + (dist / E.W) * 0.13 + rnd() * 0.08,
          jx: gauss() * 4.5 * s, jy: gauss() * 2.2 * s, w: 0.85 + rnd() * 0.3,
        })
      }
      E.grains += grains.length
      E.anims.push({ id: it.id, el, born, grains })
      E.spout = Math.min(1.3, E.spout + 0.55)
      E.lean += (clamp01((wcx - E.W / 2) / E.W + 0.5) - 0.5) * 0.03
    }

    function grainPos(g: Grain, t: number, out: number[]) {
      const u = (t - g.t0) / g.dur
      if (u >= 1) {
        const k = 1 - Math.exp(-(t - g.t0 - g.dur) * 13)
        out[0] = g.tx + g.jx * (1 - k); out[1] = g.ty + g.jy * (1 - k)
        return 2
      }
      if (u <= 0) { out[0] = g.sx; out[1] = g.sy; return 0 }
      const e = 1 - Math.pow(1 - Math.pow(u, 1.4), 2.3)
      const a = (1 - e) * (1 - e), b = 2 * (1 - e) * e, c = e * e
      out[0] = a * g.sx + b * g.cx + c * (g.tx + g.jx)
      out[1] = a * g.sy + b * g.cy + c * (g.ty + g.jy)
      return 1
    }

    const pa = [0, 0], pb = [0, 0]
    function drawGrains(p: typeof P.current) {
      const s = E.s, T = E.T
      ctx.fillStyle = p.color
      ctx.strokeStyle = p.color
      ctx.lineWidth = Math.max(0.8, 1.15 * s)
      ctx.lineCap = "round"
      for (let wi = E.anims.length - 1; wi >= 0; wi--) {
        const w = E.anims[wi]
        const a = T - w.born
        const op = smooth(0.78, 0.97, a)
        const blur = 2.9 * s * (1 - smooth(0.9, 1.14, a))
        w.el.style.opacity = op.toFixed(3)
        w.el.style.filter = blur > 0.05 ? `blur(${blur.toFixed(2)}px)` : ""
        const dotFade = 1 - smooth(0.8, 0.98, a)
        if (a > 1.16) {
          w.el.style.opacity = ""; w.el.style.filter = ""
          E.grains -= w.grains.length
          E.anims.splice(wi, 1)
          continue
        }
        for (let k = 0; k < w.grains.length; k++) {
          const g = w.grains[k]
          const st = grainPos(g, T, pa)
          if (st === 0) continue
          if (st === 1) {
            grainPos(g, T - 0.012, pb)
            const dx = pa[0] - pb[0], dy = pa[1] - pb[1]
            const sp = Math.hypot(dx, dy)
            if (sp > 1.5 * s) {
              ctx.globalAlpha = Math.min(0.4, 0.1 + sp / (24 * s))
              ctx.beginPath(); ctx.moveTo(pb[0], pb[1]); ctx.lineTo(pa[0], pa[1]); ctx.stroke()
            }
            const z = 2.5 * s * g.w
            ctx.globalAlpha = 0.07
            ctx.beginPath(); ctx.arc(pa[0], pa[1], z * 1.3, 0, 6.2832); ctx.fill()
            ctx.globalAlpha = 1
            ctx.fillRect(pa[0] - z / 2, pa[1] - z / 2, z, z)
          } else {
            const z = 1.3 * s
            ctx.globalAlpha = 0.13 * dotFade
            ctx.beginPath(); ctx.arc(pa[0], pa[1], z * 1.5, 0, 6.2832); ctx.fill()
            ctx.globalAlpha = dotFade
            ctx.fillRect(pa[0] - z / 2, pa[1] - z / 2, z, z)
          }
        }
      }
      ctx.globalAlpha = 1
    }

    // -- words handed over by React after each commit --
    function syncWords(list: Item[], leavingNow: Item[] | null) {
      for (const it of list) {
        if (E.seen.has(it.id)) continue
        E.seen.add(it.id)
        const el = wordEls.current.get(it.id)
        if (!el || !it.fly) continue
        if (E.reduced) continue
        el.style.opacity = "0"
        launch(it, el)
      }
      if (leavingNow && !E.wipe) {
        const els = leavingNow.map((it) => wordEls.current.get(it.id)).filter(Boolean) as HTMLElement[]
        if (E.reduced || !els.length) { rec.current.leaving = null; bump() }
        else { for (const w of E.anims) { w.el.style.opacity = ""; w.el.style.filter = "" } E.anims.length = 0; E.grains = 0; E.wipe = { t0: E.T, els } }
      }
      if (E.seen.size > 4000) { E.seen = new Set(list.map((i) => i.id)) }
      kick()
    }

    // -- microphone (only when the developer turns it on, only after a press) --
    async function micStart() {
      try {
        const md = navigator.mediaDevices
        if (!md || !md.getUserMedia) throw new Error("Microphone is not available in this browser")
        const stream = await md.getUserMedia({ audio: true })
        if (!E.listening || E.dead) { stream.getTracks().forEach((t) => t.stop()); return }
        const AC = (window as any).AudioContext || (window as any).webkitAudioContext
        const ac = new AC()
        const an = ac.createAnalyser(); an.fftSize = 512
        ac.createMediaStreamSource(stream).connect(an)
        const mic: any = { stream, ac, an, buf: new Uint8Array(an.fftSize), rec: null, base: textRef.current }
        E.mic = mic
        const SR = (window as any).SpeechRecognition || (window as any).webkitSpeechRecognition
        if (SR) {
          const r = new SR()
          r.continuous = true; r.interimResults = true; r.lang = P.current.lang
          r.onresult = (ev: any) => {
            let said = ""
            for (let i = 0; i < ev.results.length; i++) said += ev.results[i][0].transcript + " "
            said = said.replace(/\s+/g, " ").trim()
            api.current.setAll(mic.base && said ? mic.base + " " + said : mic.base || said)
          }
          r.onerror = (ev: any) => { if (ev && ev.error !== "no-speech" && ev.error !== "aborted") P.current.onError?.(new Error("Speech recognition: " + ev.error)) }
          try { r.start(); mic.rec = r } catch { /* already started */ }
        }
      } catch (err) {
        P.current.onError?.(err instanceof Error ? err : new Error(String(err)))
        api.current.stop(true)
      }
    }
    function micStop() {
      const mic = E.mic
      if (!mic) return
      E.mic = null; E.micLevel = 0
      try { mic.rec && mic.rec.stop() } catch { /* not running */ }
      try { mic.stream.getTracks().forEach((t: MediaStreamTrack) => t.stop()) } catch { /* gone */ }
      try { mic.ac.close() } catch { /* closed */ }
    }

    // -- state changes from React --
    function setPhase(lis: boolean, proc: boolean) {
      const p = P.current
      if (lis !== E.listening) {
        E.listening = lis
        E.bv += lis ? 2.2 : -1.6
        if (lis) {
          E.listenAt = E.T; E.secs = 0; setSecs(0)
          if (E.rise < 0.01) E.floorW = 0
          E.spout = Math.max(E.spout, 0.35)
          if (p.microphone) micStart()
          if (p.demo) {
            const d = E.demo
            d.speakAt = d.scripted ? d.nextStart : E.T
            d.state = "speak"; d.idx = 0; d.pressed = false; d.asked = false; d.scripted = false
            d.plan = planDemo(p.demoText)
            const n = d.plan.length
            d.stopRel = n >= 2 ? d.plan[n - 2].at + 0.02 : n ? d.plan[0].at + 0.4 : 1
            E.listenAt = d.speakAt
            if (textRef.current) api.current.setAll("")
          }
        } else {
          micStop()
          let at = E.T
          if (p.demo) {
            const d = E.demo
            at = d.stopT = d.scripted ? d.speakAt + d.stopRel : E.T
            d.state = "after"; d.scripted = false; d.cleared = false; d.pressed = false; d.asked = false
            d.plan = d.plan.filter((w, i) => i < d.idx || w.at <= at - d.speakAt + 0.5)
          }
          if (!p.processingControlled && !p.listeningControlled) E.procEnd = at + (p.demo ? 2.4 : 0.9)
        }
      }
      E.processing = proc
      kick()
    }

    function pressFx() { E.pressed = true; E.pressUntil = E.T + 0.11; kick() }

    // The demo is a script on the engine clock: press, speak, stop, finish, hold, wipe, rest.
    // Every time is counted from the scripted start, so one loop lasts exactly as long as the next.
    function stepDemo(p: typeof P.current) {
      const d = E.demo, T = E.T
      if (d.state !== "rest") while (d.idx < d.plan.length && T - d.speakAt >= d.plan[d.idx].at) { api.current.append(d.plan[d.idx].w); d.idx++ }
      if (p.listeningControlled) return
      if (d.state === "rest") {
        if (E.reduced || E.listening) return
        if (!d.pressed && T >= d.nextStart - 0.1) { d.pressed = true; pressFx() }
        if (!d.asked && T >= d.nextStart) { d.asked = true; d.scripted = true; api.current.start(true) }
      } else if (d.state === "speak") {
        if (!d.pressed && T - d.speakAt >= d.stopRel - 0.1) { d.pressed = true; pressFx() }
        if (!d.asked && T - d.speakAt >= d.stopRel) { d.asked = true; d.scripted = true; api.current.stop(true) }
      } else if (d.state === "after") {
        if (E.listening) return
        if (!d.cleared && T >= d.stopT + 3.45) { d.cleared = true; if (!E.reduced) api.current.setAll("") }
        if (T >= d.stopT + 3.8) { d.state = "rest"; d.nextStart = d.stopT + 5.6; d.pressed = false; d.asked = false }
      }
    }

    function demoLevel() {
      const d = E.demo, t = E.T - d.speakAt
      if (d.state !== "speak" && d.state !== "after") return 0
      const n = d.plan.length
      const end = n ? d.plan[n - 1].at - 0.15 : 2
      if (t < 0.42 || t > end) return 0.03
      let env = 1
      for (let i = 0; i < n - 1; i++) { if (/[.!?]$/.test(d.plan[i].w)) { const c = d.plan[i].at + 0.02; env = Math.min(env, 0.3 + 0.7 * smooth(0.12, 0.42, Math.abs(t - c))) } }
      const v = 0.7 + 0.2 * Math.sin(t * 6.9 + 1.1) + 0.15 * Math.sin(t * 11.3 + 1.7) + 0.1 * Math.sin(t * 17.9 + 0.2)
      return clamp01(v * env * smooth(0.42, 0.5, t))
    }

    // -- frame --
    function frame(ts: number) {
      E.raf = 0
      if (E.dead) return
      const p = P.current
      const dt = E.last ? Math.min(0.05, Math.max(0.001, (ts - E.last) / 1000)) : 1 / 60
      E.last = ts
      E.T += dt
      const T = E.T

      if (p.demo) stepDemo(p)
      if (E.pressUntil > 0 && T >= E.pressUntil) { E.pressed = false; E.pressUntil = -1 }
      if (E.procEnd > 0 && T >= E.procEnd) { E.procEnd = -1; setProcInner(false) }

      // level
      let target = 0
      if (E.listening) {
        if (typeof p.level === "number") target = clamp01(p.level)
        else if (E.mic) {
          const mic = E.mic
          mic.an.getByteTimeDomainData(mic.buf)
          let sum = 0
          for (let i = 0; i < mic.buf.length; i++) { const v = (mic.buf[i] - 128) / 128; sum += v * v }
          E.micLevel += (clamp01(Math.sqrt(sum / mic.buf.length) * 4.2) - E.micLevel) * 0.5
          target = E.micLevel
        } else if (p.demo) target = demoLevel()
        else target = 0.14 + 0.07 * Math.sin(T * 2.3)
      } else if (E.processing) target = 0.05 + 0.04 * Math.sin(T * 3.1)

      const want = E.listening || E.processing ? 1 : 0
      if (E.reduced) { E.rise = want; E.floorW = want; E.L += (target - E.L) * Math.min(1, dt * 3); E.spout = 0 }
      else {
        E.rise += (want - E.rise) * (1 - Math.exp(-dt * (want ? 5 : 4.2)))
        if (want) E.floorW += (1 - E.floorW) * (1 - Math.exp(-dt * 6))
        E.Lv += (240 * (target - E.L) - 20 * E.Lv) * dt
        E.L += E.Lv * dt
        E.spout *= Math.exp(-dt * 3.4)
        E.lean *= Math.exp(-dt * 2.5)
      }
      if (!want && E.rise < 0.004) { E.rise = 0; E.floorW = 0 }
      E.hover += ((T - E.hoverT < 0.25 ? 1 : 0) - E.hover) * (1 - Math.exp(-dt * 9))

      // timer
      if (E.listening) { const sc = Math.floor(T - E.listenAt); if (sc !== E.secs) { E.secs = sc; setSecs(sc) } }

      // button spring and status dots
      const bt = E.pressed ? 0.92 : 1
      E.bv += (620 * (bt - E.bs) - 26 * E.bv) * dt
      E.bs += E.bv * dt
      const btn = btnRef.current
      if (btn) btn.style.transform = Math.abs(E.bs - 1) > 0.0008 ? `scale(${E.bs.toFixed(4)})` : ""
      const rd = recDotRef.current, pd = procDotRef.current
      if (rd) rd.style.transform = `scale(${(E.reduced ? 1 : 1 + 0.3 * clamp01(E.L)).toFixed(3)})`
      if (pd) pd.style.transform = `scale(${(E.reduced ? 1 : 1.12 + 0.2 * Math.sin(T * 4.6)).toFixed(3)})`
      const th = thinkRef.current
      if (th && E.processing) {
        const kids = th.children
        for (let i = 0; i < kids.length; i++) {
          const ph = (T * 1.25 - i * 0.2) % 1
          const o = E.reduced ? 0.9 : 0.5 + 0.5 * smooth(0, 0.3, ph < 0.5 ? ph : 1 - ph);
          (kids[i] as HTMLElement).style.opacity = o.toFixed(3)
        }
      }

      // wipe
      if (E.wipe) {
        const w = E.wipe, n = w.els.length
        const a = T - w.t0
        for (let i = 0; i < n; i++) {
          const k = smooth(0, 1, (a - 0.3 * (i / Math.max(1, n - 1))) / 0.22)
          w.els[i].style.opacity = (1 - k).toFixed(3)
          w.els[i].style.filter = k > 0.01 ? `blur(${(3.2 * E.s * k).toFixed(2)}px)` : ""
        }
        if (a > 0.56) { E.wipe = null; rec.current.leaving = null; bump() }
      }

      // paint
      ctx.setTransform(E.dpr, 0, 0, E.dpr, 0, 0)
      ctx.clearRect(0, 0, E.W, E.H)
      drawMound(p)
      if (E.anims.length) drawGrains(p)

      const busy = E.listening || E.processing || E.rise > 0 || E.anims.length > 0 || !!E.wipe || Math.abs(E.bs - 1) > 0.001 || Math.abs(E.bv) > 0.01 || E.pressed || E.hover > 0.01 || (p.demo && !E.reduced)
      if (busy && E.onScreen && !document.hidden) { if (!E.raf) E.raf = requestAnimationFrame(frame) }
      else if (!E.raf) E.last = 0
    }
    function kick() { if (!E.raf && !E.dead && E.onScreen && !document.hidden) { E.raf = requestAnimationFrame(frame) } }

    const ro = typeof ResizeObserver !== "undefined" ? new ResizeObserver(measure) : null
    ro?.observe(root)
    measure()
    const io = typeof IntersectionObserver !== "undefined" ? new IntersectionObserver((en) => { E.onScreen = en[en.length - 1].isIntersecting; if (E.onScreen) kick() }) : null
    io?.observe(root)
    const onVis = () => { if (!document.hidden) { E.last = 0; kick() } }
    document.addEventListener("visibilitychange", onVis)
    const onMq = () => {
      E.reduced = !!(mq && mq.matches)
      if (E.reduced) { for (const w of E.anims) { w.el.style.opacity = ""; w.el.style.filter = "" } E.anims.length = 0; E.grains = 0 }
      bump(); kick()
    }
    mq?.addEventListener?.("change", onMq)
    const onMove = (ev: PointerEvent) => { const r = box.getBoundingClientRect(); E.px = ev.clientX - r.left; if (ev.clientY - r.top > r.height * 0.35) E.hoverT = E.T; kick() }
    box.addEventListener("pointermove", onMove)

    Object.assign(E, { syncWords, setPhase, measure, kick, pressFx })
    rec.current.mounted = true
    ;(E as any).setPhase(P.current.isListening, P.current.isProcessing)
    // Under reduced motion the demo rests on its finished sentence instead of playing.
    if (E.reduced && P.current.demo && !textRef.current && !P.current.controlledText) { textRef.current = P.current.demoText; setInner(P.current.demoText) }
    kick()

    return () => {
      E.dead = true
      cancelAnimationFrame(E.raf)
      ro?.disconnect(); io?.disconnect()
      document.removeEventListener("visibilitychange", onVis)
      mq?.removeEventListener?.("change", onMq)
      box.removeEventListener("pointermove", onMove)
      micStop()
      rec.current.mounted = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useIsoLayoutEffect(() => { eng.current?.measure() }, [size])
  useIsoLayoutEffect(() => { eng.current?.syncWords(items, leaving) })
  React.useEffect(() => { eng.current?.setPhase(isListening, isProcessing) }, [isListening, isProcessing])
  React.useEffect(() => { eng.current?.kick() }, [look, level, color, glyphs, demo])

  const onKeyDown = (ev: React.KeyboardEvent) => {
    if (ev.key === "Escape" && isListening) { ev.preventDefault(); api.current.stop() }
  }
  const press = (on: boolean) => { const E = eng.current; if (E) { E.pressed = on; E.pressUntil = -1; E.kick() } }

  const s = geo.s
  const c = `lsdd${uid}`
  const mode = isListening ? "listening" : isProcessing ? "processing" : "idle"
  const empty = shown.length === 0
  const mm = Math.floor(secs / 60), ss = secs % 60
  const css = `
.${c}{position:relative;width:100%;height:100%;min-height:360px;display:flex;align-items:center;justify-content:center;box-sizing:border-box;overflow:hidden;-webkit-font-smoothing:antialiased;-webkit-tap-highlight-color:transparent}
.${c} *{box-sizing:border-box}
.${c}-box{position:relative;flex:none;border-radius:calc(var(--s)*23px);overflow:hidden}
.${c}-cv{position:absolute;left:0;top:0;width:100%;height:100%;pointer-events:none;z-index:2}
.${c}-glow{position:absolute;pointer-events:none;z-index:0;opacity:0.5;filter:blur(calc(var(--s)*4.5px))}
.${c}-text{position:absolute;z-index:1;left:calc(var(--s)*18px);top:calc(var(--s)*15.75px);right:calc(var(--s)*72px);margin:0;font-size:calc(var(--s)*16px);line-height:calc(var(--s)*20.5px);font-weight:400;letter-spacing:0;white-space:normal;overflow-wrap:break-word;-webkit-user-select:text;user-select:text;cursor:text}
.${c}-w{display:inline-block;white-space:pre;will-change:opacity,filter}
.${c}-ph{position:absolute;z-index:1;left:calc(var(--s)*18px);top:calc(var(--s)*15.75px);right:calc(var(--s)*72px);font-size:calc(var(--s)*16px);line-height:calc(var(--s)*20.5px);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;pointer-events:none;opacity:0;transition:opacity .14s linear}
.${c}-ph[data-on="1"]{opacity:1;transition:opacity .32s ease .5s}
.${c}-st{position:absolute;z-index:3;right:calc(var(--s)*18px);top:calc(var(--s)*15.75px);height:calc(var(--s)*20.5px);display:flex;align-items:center;gap:calc(var(--s)*7px);font-size:calc(var(--s)*11px);line-height:1;font-variant-numeric:tabular-nums;white-space:nowrap;pointer-events:none;opacity:0;transition:opacity .2s ease}
.${c}-st[data-on="1"]{opacity:1}
.${c}-dot{width:calc(var(--s)*4.2px);height:calc(var(--s)*4.2px);border-radius:50%;flex:none}
.${c}-btn{position:absolute;z-index:3;right:calc(var(--s)*14px);bottom:calc(var(--s)*20px);width:calc(var(--s)*41px);height:calc(var(--s)*41px);border-radius:50%;padding:0;margin:0;border:1px solid rgba(255,255,255,0.07);cursor:pointer;display:grid;place-items:center;-webkit-appearance:none;appearance:none;touch-action:manipulation;transition:filter .18s ease;outline:none}
.${c}-btn:hover{filter:brightness(1.14)}
.${c}-btn:focus-visible{box-shadow:0 0 0 calc(var(--s)*2px) ${surface},0 0 0 calc(var(--s)*4px) ${color}}
.${c}-btn:disabled{cursor:default;opacity:.5;filter:none}
.${c}-ic{grid-area:1/1;display:grid;place-items:center;opacity:0;transform:scale(.55);filter:blur(calc(var(--s)*2px));transition:opacity .2s ease,transform .26s cubic-bezier(.3,1.5,.5,1),filter .2s ease}
.${c}-ic[data-on="1"]{opacity:1;transform:scale(1);filter:blur(0)}
.${c}-sr{position:absolute;width:1px;height:1px;margin:-1px;padding:0;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap;border:0}
@media (prefers-reduced-motion:reduce){.${c}-ph,.${c}-st,.${c}-ic,.${c}-btn{transition:none!important}}
`
  const mergedStyle = { background, fontFamily: font, ...style, ["--s" as any]: s } as React.CSSProperties

  return (
    <div ref={rootRef} className={className ? `${c} ${className}` : c} style={mergedStyle} role="group" aria-label={ariaLabel} onKeyDown={onKeyDown} data-mode={mode}>
      <style>{css}</style>
      <div ref={boxRef} className={`${c}-box`} style={{ width: geo.w, height: geo.h, background: surface, border: `1px solid ${border}` }}>
        <canvas ref={glowRef} className={`${c}-glow`} aria-hidden="true" />
        <div className={`${c}-ph`} data-on={empty ? "1" : "0"} style={{ color: muted }} aria-hidden="true">{isListening ? listeningLabel : isProcessing ? "" : placeholder}</div>
        <p className={`${c}-text`} style={{ color }} role="log" aria-live="polite" aria-label="Transcript">
          {shown.map((it, i) => (
            <React.Fragment key={it.id}>
              <span className={`${c}-w`} ref={(el) => { if (el) wordEls.current.set(it.id, el); else wordEls.current.delete(it.id) }}>{it.text}</span>
              {i < shown.length - 1 ? " " : null}
            </React.Fragment>
          ))}
        </p>
        <canvas ref={cvRef} className={`${c}-cv`} aria-hidden="true" />
        <div className={`${c}-st`} data-on={mode === "listening" ? "1" : "0"} style={{ color: muted }} aria-hidden="true">
          <span ref={recDotRef} className={`${c}-dot`} style={{ background: color }} />
          <span>{mm}:{ss < 10 ? "0" : ""}{ss}</span>
        </div>
        <div className={`${c}-st`} data-on={mode === "processing" ? "1" : "0"} style={{ color: muted }} aria-hidden="true">
          <span ref={procDotRef} className={`${c}-dot`} style={{ background: color }} />
          <span>{processingLabel}</span>
        </div>
        <button
          ref={btnRef}
          type="button"
          className={`${c}-btn`}
          style={{ background: buttonColor, color }}
          disabled={disabled}
          aria-pressed={isListening}
          aria-label={isListening ? "Stop dictation" : "Start dictation"}
          aria-busy={isProcessing || undefined}
          onClick={() => api.current.toggle()}
          onPointerDown={() => press(true)}
          onPointerUp={() => press(false)}
          onPointerLeave={() => press(false)}
          onPointerCancel={() => press(false)}
          onKeyDown={(ev) => { if (ev.key === " " || ev.key === "Enter") press(true) }}
          onKeyUp={() => press(false)}
          onBlur={() => press(false)}
        >
          <span className={`${c}-ic`} data-on={mode === "idle" ? "1" : "0"} aria-hidden="true">
            <svg width={14 * s} height={16 * s} viewBox="0 0 14 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
              <rect x="4.6" y="1" width="4.8" height="8.6" rx="2.4" />
              <path d="M2 7.4a5 5 0 0 0 10 0" />
              <path d="M7 12.4V15" />
            </svg>
          </span>
          <span className={`${c}-ic`} data-on={mode === "listening" ? "1" : "0"} aria-hidden="true">
            <span style={{ width: 8.6 * s, height: 8.6 * s, borderRadius: 2.2 * s, background: color, display: "block" }} />
          </span>
          <span className={`${c}-ic`} data-on={mode === "processing" ? "1" : "0"} aria-hidden="true">
            <span ref={thinkRef} style={{ display: "flex", gap: 2.2 * s }}>
              <i style={{ width: 2.7 * s, height: 2.7 * s, borderRadius: "50%", background: color, display: "block" }} />
              <i style={{ width: 2.7 * s, height: 2.7 * s, borderRadius: "50%", background: color, display: "block" }} />
              <i style={{ width: 2.7 * s, height: 2.7 * s, borderRadius: "50%", background: color, display: "block" }} />
            </span>
          </span>
        </button>
        <span className={`${c}-sr`} role="status">{mode === "listening" ? "Listening" : mode === "processing" ? "Finishing" : ""}</span>
      </div>
    </div>
  )
})

export default DictationDune
