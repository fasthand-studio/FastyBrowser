import * as React from "react"
import { createRoot } from "react-dom/client"
import { DictationDune } from "./dictation-dune"

/*
  Voice search for Fasty on Windows. The animation is Dictation Dune; the words come from the
  speech recognizer that ships with Windows, run by the app (see voice/voice.ps1). The app sends
  {t:"interim"|"final"|"level"|"error"|"end"} events to window.fastyVoice and starts and stops
  listening through rpc("voice.start") and rpc("voice.stop").
*/

declare global {
  interface Window {
    fastyVoice: (e: VoiceEvent) => void
    rpc: (m: string, p?: unknown) => Promise<unknown>
    FastyVoice: { open: (o: OpenOptions) => void; close: () => void }
  }
}

type VoiceEvent = { t: string; v?: string | number; lang?: string }
type OpenOptions = { auto?: boolean; dark: boolean; onSubmit: (text: string) => void; onCancel: () => void }

const SILENCE_MS = 1700 // stop on its own this long after the last word

const join = (a: string, b: string) => (a && b ? a + " " + b : a || b)

function VoiceApp({ auto, dark, onSubmit, onCancel }: OpenOptions) {
  const [listening, setListening] = React.useState(false)
  const [text, setText] = React.useState("")
  const [level, setLevel] = React.useState(0)
  const [error, setError] = React.useState("")
  const finals = React.useRef("")
  const interim = React.useRef("")
  const listeningRef = React.useRef(false)
  const timer = React.useRef(0)

  const finish = React.useCallback(
    (t: string) => {
      window.clearTimeout(timer.current)
      if (listeningRef.current) { listeningRef.current = false; window.rpc("voice.stop") }
      setListening(false)
      const q = t.trim()
      if (q) onSubmit(q)
    },
    [onSubmit],
  )

  const arm = React.useCallback(() => {
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => {
      const t = join(finals.current, interim.current)
      if (listeningRef.current && t.trim()) finish(t)
    }, SILENCE_MS)
  }, [finish])

  const start = React.useCallback(() => {
    finals.current = ""
    interim.current = ""
    setText("")
    setError("")
    listeningRef.current = true
    setListening(true)
    if (!/[?&]selftest=1/.test(location.search)) window.rpc("voice.start", { lang: navigator.language || "en-US" })
  }, [])

  React.useEffect(() => {
    window.fastyVoice = (e) => {
      if (e.t === "interim") { interim.current = String(e.v || ""); setText(join(finals.current, interim.current)); arm() }
      else if (e.t === "final") { finals.current = join(finals.current, String(e.v || "")); interim.current = ""; setText(finals.current); arm() }
      else if (e.t === "level") setLevel(Math.min(1, Number(e.v || 0) * 2.2))
      else if (e.t === "error") { listeningRef.current = false; setListening(false); setError(String(e.v || "Voice search failed.")) }
      else if (e.t === "end") { if (listeningRef.current) { listeningRef.current = false; setListening(false) } }
    }
    const onKey = (ev: KeyboardEvent) => { if (ev.key === "Escape") { window.rpc("voice.stop"); onCancel() } }
    window.addEventListener("keydown", onKey)
    if (auto) start()
    return () => { window.removeEventListener("keydown", onKey); window.clearTimeout(timer.current); window.rpc("voice.stop") }
  }, [])

  const c = dark
    ? { color: "#ededed", muted: "#949494", surface: "#1e1e1e", border: "#333333", buttonColor: "#343434" }
    : { color: "#171717", muted: "#8c8c8c", surface: "#ffffff", border: "#e8e8e8", buttonColor: "#ececec" }

  return (
    <div style={{ display: "grid", justifyItems: "center", gap: 14 }}>
      <DictationDune
        look="glyphs"
        value={text}
        listening={listening}
        level={level}
        onStart={start}
        onStop={(t) => finish(t || join(finals.current, interim.current))}
        placeholder="Press the mic and say what to search"
        listeningLabel="Listening..."
        processingLabel="searching"
        ariaLabel="Voice search"
        {...c}
        style={{ width: "min(640px, 94vw)", height: 300, minHeight: 0 }}
      />
      <p role={error ? "alert" : "status"} style={{ margin: 0, minHeight: 20, maxWidth: 560, textAlign: "center", fontSize: 13, color: error ? (dark ? "#f28b82" : "#b3261e") : c.muted }}>
        {error || (listening ? "Speak now. It searches when you stop talking." : "Audio is turned into text on this PC. Nothing is sent anywhere.")}
      </p>
      <button
        onClick={onCancel}
        style={{ font: "inherit", fontSize: 13, color: c.muted, background: "transparent", border: 0, cursor: "pointer", padding: "6px 12px", borderRadius: 8 }}
      >
        Cancel (Esc)
      </button>
    </div>
  )
}

let root: ReturnType<typeof createRoot> | null = null
let host: HTMLElement | null = null

window.FastyVoice = {
  open(o) {
    window.FastyVoice.close()
    host = document.createElement("div")
    host.id = "voice-overlay"
    host.setAttribute("role", "dialog")
    host.setAttribute("aria-modal", "true")
    host.setAttribute("aria-label", "Voice search")
    Object.assign(host.style, {
      position: "fixed", inset: "0", zIndex: "50", display: "grid", placeItems: "center",
      background: o.dark ? "rgba(19,19,19,0.96)" : "rgba(244,244,244,0.96)",
    })
    document.body.appendChild(host)
    root = createRoot(host)
    root.render(<VoiceApp {...o} />)
  },
  close() {
    root?.unmount()
    host?.remove()
    root = null
    host = null
  },
}
