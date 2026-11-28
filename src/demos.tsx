import { useEffect, useState } from "react";
import type { IconId } from "icon-tree-animate";
import { BrowserDots, BrandMark, FeatureStatus, TreeIcon } from "./components";

const previewScenes: { id: string; title: string; description: string; icon: IconId }[] = [
  { id: "home", title: "A quieter new tab", description: "Search, pinned sites, and the tools you use—without the clutter.", icon: "search-normal" },
  { id: "sleep", title: "Tabs that rest", description: "Unused tabs sleep in the background and wake when you return.", icon: "moon" },
  { id: "shield", title: "Shield on by default", description: "Ads and trackers can be stopped before a request is made.", icon: "shield-tick" },
  { id: "extensions", title: "Extensions, your call", description: "A local store for WebExtension-style additions you choose to keep.", icon: "component" },
  { id: "agent", title: "Agent control, with permission", description: "Local request tools for sites you own or are allowed to test.", icon: "code" },
  { id: "authenticator", title: "Passkeys and one-time codes", description: "Keep authenticator secrets encrypted on your own computer.", icon: "finger-scan" },
];

function MockSidebar({ active = "home", dark = false }: { active?: string; dark?: boolean }) {
  const items: { id: string; label: string; icon: IconId }[] = [
    { id: "home", label: "New tab", icon: "add" },
    { id: "shield", label: "Privacy", icon: "shield-tick" },
    { id: "extensions", label: "Extensions", icon: "component" },
    { id: "agent", label: "Agent control", icon: "code" },
    { id: "authenticator", label: "Authenticator", icon: "finger-scan" },
  ];
  return (
    <aside className={`mock-sidebar ${dark ? "is-dark" : ""}`}>
      <div className="mock-sidebar-brand"><BrandMark size={22} /><strong>Fasty</strong></div>
      <div className="mock-sidebar-label">TOOLS</div>
      {items.map((item) => (
        <div className={`mock-sidebar-item ${active === item.id ? "is-active" : ""}`} key={item.id}>
          <TreeIcon id={item.icon} size={15} dark={dark} /><span>{item.label}</span>
        </div>
      ))}
      <div className="mock-sidebar-bottom"><TreeIcon id="shield-tick" size={15} color="#0a9809" dark={dark} /><span>Shield is on</span></div>
    </aside>
  );
}

function MockToolbar({ title = "New tab" }: { title?: string }) {
  return (
    <div className="mock-toolbar">
      <BrowserDots />
      <div className="mock-nav"><TreeIcon id="arrow-right" size={14} className="is-back" /><TreeIcon id="arrow-right" size={14} /></div>
      <div className="mock-address"><TreeIcon id="search-normal" size={14} /><span>{title}</span></div>
      <TreeIcon id="more" size={16} />
    </div>
  );
}

function MockTabs({ sleeping = false }: { sleeping?: boolean }) {
  const tabs = ["Fasty Handbook", "Design notes", "WebKitGTK", "MDN Web Docs"];
  return (
    <div className="mock-tabs">
      {tabs.map((tab, index) => (
        <div className={`mock-tab ${index === 0 ? "is-active" : ""} ${sleeping && index > 1 ? "is-sleeping" : ""}`} key={tab}>
          <span className="mock-site-dot" data-site={index} />
          <span>{tab}</span>
          {sleeping && index > 1 ? <TreeIcon id="moon" size={12} color="#8e6bd8" /> : null}
        </div>
      ))}
    </div>
  );
}

function HomeMock({ dark = false }: { dark?: boolean }) {
  return (
    <div className={`mock-home ${dark ? "is-dark" : ""}`}>
      <BrandMark size={42} />
      <h3>Where do you want to go?</h3>
      <div className="mock-search"><TreeIcon id="search-normal" size={17} dark={dark} /><span>Search or enter an address</span></div>
      <div className="mock-shortcuts">
        {["GitHub", "Wikipedia", "MDN", "Hacker News"].map((name, index) => (
          <div key={name}><span className="mock-site-dot" data-site={index} /><span>{name}</span></div>
        ))}
      </div>
      <div className="mock-home-cards">
        <div><TreeIcon id="shield-tick" size={17} color="#0a9809" dark={dark} /><strong>Privacy first</strong><span>Shield is on</span></div>
        <div><TreeIcon id="moon" size={17} color="#8e6bd8" dark={dark} /><strong>Room to breathe</strong><span>2 tabs resting</span></div>
      </div>
    </div>
  );
}

function SleepMock() {
  return (
    <div className="mock-feature-page">
      <div className="mock-page-heading"><TreeIcon id="moon" size={22} color="#8e6bd8" /><div><h3>Memory & speed</h3><p>Quiet tabs use less memory.</p></div></div>
      <div className="sleep-illustration">
        {["Design notes", "WebKitGTK", "MDN Web Docs"].map((name, index) => (
          <div className={index ? "is-resting" : "is-active"} key={name}>
            <span className="mock-site-dot" data-site={index + 1} /><span>{name}</span>
            <small>{index ? "Resting" : "Active"}</small>
            {index ? <TreeIcon id="moon" size={14} color="#8e6bd8" /> : <TreeIcon id="play" size={14} color="#0a9809" />}
          </div>
        ))}
      </div>
      <div className="mock-memory"><span>Relative memory load</span><div><i /><i /><i /><i /></div><small>Illustrative, not a benchmark</small></div>
    </div>
  );
}

function ShieldMock() {
  return (
    <div className="mock-feature-page">
      <div className="mock-page-heading"><TreeIcon id="shield-tick" size={22} color="#0a9809" /><div><h3>Privacy</h3><p>Shield is protecting this session.</p></div></div>
      <div className="shield-score"><strong>18</strong><span>ads & trackers blocked</span><FeatureStatus>Shield on</FeatureStatus></div>
      <div className="shield-rules">
        {["Ads", "Trackers", "Fingerprinting"].map((label) => <div key={label}><TreeIcon id="check" size={15} color="#0a9809" /><span>{label}</span><small>Blocked</small></div>)}
      </div>
      <p className="mock-disclaimer">Demo data · no live requests are inspected</p>
    </div>
  );
}

function ExtensionsMock() {
  return (
    <div className="mock-feature-page">
      <div className="mock-page-heading"><TreeIcon id="component" size={22} color="#5301fe" /><div><h3>Extensions</h3><p>Small additions. Your choice.</p></div></div>
      <div className="mock-extension-grid">
        {[
          { icon: "book" as IconId, name: "Reader mode", detail: "FastHand Studio" },
          { icon: "eye-slash" as IconId, name: "Quiet colors", detail: "Fasty community" },
          { icon: "note" as IconId, name: "Local notes", detail: "Fasty community" },
          { icon: "routing" as IconId, name: "Request inspector", detail: "FastHand Studio" },
        ].map((extension, index) => (
          <div key={extension.name}><TreeIcon id={extension.icon} size={20} color={index % 2 ? "#0a9809" : "#5301fe"} /><strong>{extension.name}</strong><span>{extension.detail}</span><small>{index === 0 ? "Installed" : "Add"}</small></div>
        ))}
      </div>
    </div>
  );
}

function AgentMock() {
  return (
    <div className="mock-feature-page">
      <div className="authorization-note"><TreeIcon id="lock" size={17} color="#8e6bd8" /><span>Only for sites you own or are allowed to test.</span></div>
      <div className="mock-page-heading"><TreeIcon id="code" size={22} color="#5301fe" /><div><h3>Agent control</h3><p>Local tools for careful security testing.</p></div><FeatureStatus>Opt-in</FeatureStatus></div>
      <div className="request-list">
        <div><span className="method get">GET</span><code>shop.example/api/product/42</code><small>Complete</small></div>
        <div className="is-held"><span className="method post">POST</span><code>shop.example/api/checkout</code><small>Held</small></div>
        <div><span className="method get">GET</span><code>analytics.example/track</code><small>Blocked</small></div>
        <div><span className="method get">GET</span><code>shop.example/api/inventory</code><small>Complete</small></div>
      </div>
      <div className="redaction-line"><TreeIcon id="eye-slash" size={15} color="#0a9809" /><span>Secrets redacted before agent output</span></div>
    </div>
  );
}

function AuthenticatorMock() {
  return (
    <div className="mock-feature-page authenticator-mock">
      <div className="mock-page-heading"><TreeIcon id="finger-scan" size={22} color="#0a9809" /><div><h3>Authenticator</h3><p>Codes stay on this computer.</p></div></div>
      <div className="code-card"><span>DEMO ACCOUNT</span><strong>482 915</strong><div><i /><i /><i /><i /><i /><i /></div><small>Demo code · no secret stored</small></div>
      <div className="local-note"><TreeIcon id="lock" size={16} color="#0a9809" /><span>Encrypted local storage</span></div>
    </div>
  );
}

function SceneContent({ scene }: { scene: string }) {
  if (scene === "sleep") return <SleepMock />;
  if (scene === "shield") return <ShieldMock />;
  if (scene === "extensions") return <ExtensionsMock />;
  if (scene === "agent") return <AgentMock />;
  if (scene === "authenticator") return <AuthenticatorMock />;
  return <HomeMock />;
}

export function HeroBrowser() {
  return (
    <div className="hero-browser" aria-label="Fasty Browser interface preview">
      <div className="hero-browser-bar"><BrandMark size={17} /><strong>Fasty</strong><span>Browser</span><BrowserDots /></div>
      <MockToolbar />
      <div className="hero-browser-body"><MockSidebar /><div className="hero-browser-content"><MockTabs sleeping /><HomeMock /></div></div>
    </div>
  );
}

export function ProductPreview() {
  const [scene, setScene] = useState("home");
  const current = previewScenes.find((item) => item.id === scene) ?? previewScenes[0];
  return (
    <div className="product-preview">
      <div className="preview-scene-list" aria-label="Choose a product preview">
        {previewScenes.map((item, index) => (
          <button key={item.id} className={scene === item.id ? "is-active" : ""} aria-pressed={scene === item.id} onClick={() => setScene(item.id)}>
            <span className="preview-number">0{index + 1}</span>
            <TreeIcon id={item.icon} size={20} dark />
            <span><strong>{item.title}</strong><small>{item.description}</small></span>
            <TreeIcon id="arrow-right" size={17} dark />
          </button>
        ))}
      </div>
      <div className="preview-stage">
        <div className="preview-stage-label"><span className="live-dot" />Interactive product preview<span>Simulated interface</span></div>
        <div className="preview-browser" key={scene}>
          <div className="preview-browser-title"><BrandMark size={16} /><strong>Fasty</strong><span>{current.title}</span><BrowserDots /></div>
          <MockToolbar title={current.title} />
          <div className="preview-browser-body"><MockSidebar active={scene} /><div className="preview-browser-content"><MockTabs sleeping={scene === "sleep"} /><div className="preview-scene-content"><SceneContent scene={scene} /></div></div></div>
        </div>
        <p className="preview-disclaimer">A visual demonstration of the Fasty experience. No live browsing is performed.</p>
      </div>
    </div>
  );
}

export function SleepDemo() {
  const [sleeping, setSleeping] = useState(false);
  return (
    <div className="demo-card sleep-demo">
      <div className="demo-copy"><div className="card-head"><TreeIcon id="moon" size={25} color="#8e6bd8" mode="hover" /><h3>Let quiet tabs rest.</h3></div><p>See how sleeping tabs can reduce the memory pressure from background pages.</p></div>
      <div className="sleep-tabs">
        {["Article", "Docs", "Repository", "Reference"].map((name, index) => (
          <div className={sleeping && index > 0 ? "is-sleeping" : ""} key={name}><span className="mock-site-dot" data-site={index} /><span>{name}</span><small>{sleeping && index > 0 ? "Resting" : "Active"}</small>{sleeping && index > 0 ? <TreeIcon id="moon" size={14} color="#8e6bd8" /> : <span className="active-dot" />}</div>
        ))}
      </div>
      <div className="memory-meter"><div><span>Relative memory load</span><strong>{sleeping ? "Lower" : "Higher"}</strong></div><div className="memory-bars">{[1, 2, 3, 4, 5, 6].map((bar) => <i className={sleeping && bar > 2 ? "is-muted" : ""} key={bar} />)}</div><small>Concept demo · not a performance benchmark</small></div>
      <button className="demo-action" aria-pressed={sleeping} onClick={() => setSleeping((value) => !value)}><TreeIcon id={sleeping ? "play" : "moon"} size={17} color={sleeping ? "#5301fe" : "#8e6bd8"} />{sleeping ? "Wake background tabs" : "Let background tabs sleep"}</button>
    </div>
  );
}

export function ShieldDemo() {
  const [rules, setRules] = useState({ ads: true, trackers: true, fingerprinting: true });
  const activeCount = Object.values(rules).filter(Boolean).length;
  return (
    <div className="demo-card shield-demo">
      <div className="demo-copy"><div className="card-head"><TreeIcon id="shield-tick" size={25} color="#0a9809" mode="hover" /><h3>Block the noise early.</h3></div><p>Explore the built-in shield categories before a request reaches a site.</p></div>
      <div className="rule-list">
        {([
          ["ads", "Ads"], ["trackers", "Trackers"], ["fingerprinting", "Fingerprinting"],
        ] as const).map(([key, label]) => (
          <button key={key} className={rules[key] ? "is-on" : ""} aria-pressed={rules[key]} onClick={() => setRules((current) => ({ ...current, [key]: !current[key] }))}><span>{label}</span><small>{rules[key] ? "Blocking" : "Allowed"}</small><i /></button>
        ))}
      </div>
      <div className="shield-summary"><TreeIcon id={activeCount ? "shield-tick" : "information"} size={19} color={activeCount ? "#0a9809" : "#77726b"} /><span>{activeCount ? `${activeCount} shield ${activeCount === 1 ? "category" : "categories"} active` : "Shield categories paused"}</span></div>
      <small className="demo-footnote">Concept demo · no network traffic is inspected</small>
    </div>
  );
}

export function PipDemo() {
  const [playing, setPlaying] = useState(true);
  return (
    <div className="demo-card pip-demo">
      <div className="demo-copy"><div className="card-head"><TreeIcon id="video-play" size={25} color="#5301fe" mode="hover" /><h3>Keep watching, anywhere.</h3></div><p>A video-only window that stays above your work.</p></div>
      <div className={`pip-demo-player ${playing ? "is-playing" : ""}`}>
        <div className="pip-demo-top"><span><TreeIcon id="video-play" size={14} color="#ededed" dark /> Picture in picture</span><TreeIcon id="more" size={15} color="#ededed" dark /></div>
        <div className="pip-demo-stage"><div className="pip-orb" /><div className="pip-wave">{[1, 2, 3, 4, 5].map((bar) => <i key={bar} />)}</div><button aria-label={playing ? "Pause demo video" : "Play demo video"} onClick={() => setPlaying((value) => !value)}><TreeIcon id={playing ? "pause" : "play"} size={20} color="#ffffff" dark /></button></div>
      </div>
      <small className="demo-footnote">Visual demo · no video is streamed</small>
    </div>
  );
}

export function AuthenticatorDemo() {
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    if (!visible) return;
    const timeout = window.setTimeout(() => setVisible(false), 12_000);
    return () => window.clearTimeout(timeout);
  }, [visible]);
  return (
    <div className="demo-card authenticator-demo">
      <div className="demo-copy"><div className="card-head"><TreeIcon id="finger-scan" size={25} color="#0a9809" mode="hover" /><h3>One less app to open.</h3></div><p>Passkeys and one-time codes belong on your computer, not a remote account.</p></div>
      <div className="authenticator-code"><span>FASTY DEMO</span><strong>{visible ? "482 915" : "••• •••"}</strong><button onClick={() => setVisible((value) => !value)}><TreeIcon id={visible ? "eye-slash" : "play"} size={17} color="#5301fe" />{visible ? "Hide demo code" : "Show demo code"}</button></div>
      <div className="authenticator-local"><TreeIcon id="lock" size={16} color="#0a9809" /><span>Demo only · no real secret is stored</span></div>
    </div>
  );
}
