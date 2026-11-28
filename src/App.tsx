import { useState } from "react";
import type { IconId } from "icon-tree-animate";
import { BrandMark, FeatureStatus, publicUrl, SectionHeading, TextLink, TreeIcon } from "./components";
import { AuthenticatorDemo, HeroBrowser, PipDemo, ProductPreview, ShieldDemo, SleepDemo } from "./demos";
import { DownloadPanel, WindowsDownloadPanel } from "./downloads";

const STUDIO_URL = "https://fasthandstudio.asia/";
const REPO_URL ="https://github.com/fasthand-studio/FastyBrowser";
const INSTALL_DOC_URL = "https://github.com/fasthand-studio/FastyBrowser/blob/main/docs/install.md";
const FEATURES_DOC_URL = "https://github.com/fasthand-studio/FastyBrowser/blob/main/docs/features.md";
const GUIDE_URL = publicUrl("/fasty-product-guide.pdf");

const features: { icon: IconId; title: string; description: string; status: string; experimental?: boolean; color: string }[] = [
  { icon: "moon", title: "Tabs that sleep", description: "Unused tabs unload after five minutes by default, while tabs playing audio stay awake.", status: "Implemented", color: "#8e6bd8" },
  { icon: "shield-tick", title: "Built-in shield", description: "EasyList, EasyPrivacy, and StevenBlack filtering. YouTube ad handling is best effort, not guaranteed.", status: "Implemented", color: "#0a9809" },
  { icon: "eye-slash", title: "Privacy by default", description: "No account, telemetry, crash reporting, or analytics. Browser data stays local; sites and extensions make their own requests.", status: "Implemented", color: "#0579fe" },
  { icon: "finger-scan", title: "Passkeys & authenticator", description: "ES256 passkeys and TOTP codes with encrypted local storage. No USB security keys, sync, or QR scanning.", status: "Implemented · limited", color: "#0a9809" },
  { icon: "video-play", title: "Picture in picture", description: "A compact video-only window. Protected or cross-origin video may need a fallback mode.", status: "Site dependent", experimental: true, color: "#5301fe" },
  { icon: "component", title: "Extensions", description: "WebExtension-style installation from stores, direct URLs, or local files, with partial API compatibility.", status: "Partial compatibility", experimental: true, color: "#0579fe" },
  { icon: "code", title: "Agent control", description: "An opt-in local Unix-socket API for authorized testing of page fetch, XHR, and beacon requests, with mandatory redaction.", status: "Opt-in local API", color: "#fd970c" },
  { icon: "cpu", title: "Native WebKitGTK", description: "A Go application using WebKitGTK 6.0, GTK 4, and libadwaita—not Chromium or Electron.", status: "Implemented", color: "#5301fe" },
];

const faqs = [
  {
    question: "Which platforms does Fasty support?",
    answer: "Linux x86_64 with glibc 2.39+ is the main target, including current Arch, Ubuntu 24.04+, Debian 13+, and Fedora 40+; release notes report testing on Arch only. A Windows 10/11 (x64) preview is also available; it has been tested on Windows 11 only and has fewer features. macOS and ARM64 builds are not published.",
  },
  {
    question: "Can I download a ready-to-run binary?",
    answer: "Yes. Fasty 1.1.1 publishes AppImage, .deb, .rpm, and tarball assets for Linux x86_64, plus SHA256SUMS; the AppImage still requires WebKitGTK 6.0, GTK 4, and libadwaita. The Windows 0.1.0 preview publishes an installer and a portable zip with their own SHA-256 checksums and needs the Edge WebView2 Runtime. No macOS or ARM64 binary is published.",
  },
  {
    question: "Is Fasty based on Chromium or Electron?",
    answer: "On Linux, no: Fasty is a native browser written in Go and built on WebKitGTK 6.0 with GTK 4 and libadwaita. The Windows preview is also written in Go but draws pages with Microsoft Edge WebView2, which is Chromium-based, because WebKitGTK does not run on Windows. It does not bundle Chromium or Electron.",
  },
  {
    question: "Does Fasty require an account or send telemetry?",
    answer: "The project describes an account-free browser with no telemetry, crash reporting, analytics, or remote configuration. Its own network activity is limited to public filter-list downloads; pages, search engines, and extensions naturally make their own requests.",
  },
  {
    question: "What can I do with extensions?",
    answer: "Fasty includes WebExtension-style extension support and a local extension store. Availability and compatibility can depend on the APIs an extension uses; check the project documentation before installing a specific add-on.",
  },
  {
    question: "How does agent control stay safe?",
    answer: "Agent control is implemented as an opt-in local JSON-over-Unix-socket API, off by default. It covers page fetch, XHR, and sendBeacon traffic for sites you own or are allowed to test. Any process running as the same user can control tabs while the socket is enabled.",
  },
  {
    question: "Where can I report a problem?",
    answer: "Use the issue tracker in the official FastyBrowser repository. Include your Linux distribution, GTK/WebKitGTK versions, installation method, and clear reproduction steps.",
  },
  {
    question: "Is Fasty open source?",
    answer: "Yes. FastyBrowser is released under the MIT License. The original logo and brand assets remain attributable to FastHand Studio.",
  },
];

function GithubIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d="M12 .7a11.5 11.5 0 0 0-3.64 22.41c.58.1.79-.25.79-.56v-2.23c-3.22.7-3.9-1.37-3.9-1.37-.52-1.33-1.28-1.69-1.28-1.69-1.05-.72.08-.7.08-.7 1.16.08 1.77 1.19 1.77 1.19 1.03 1.77 2.7 1.26 3.36.96.1-.75.4-1.26.73-1.55-2.57-.29-5.27-1.29-5.27-5.69 0-1.26.45-2.28 1.19-3.09-.12-.29-.52-1.46.11-3.05 0 0 .97-.31 3.16 1.18a10.9 10.9 0 0 1 5.76 0c2.19-1.49 3.15-1.18 3.15-1.18.63 1.59.23 2.76.12 3.05.74.81 1.19 1.83 1.19 3.09 0 4.41-2.71 5.39-5.29 5.68.42.36.79 1.07.79 2.16v3.21c0 .31.21.67.8.56A11.5 11.5 0 0 0 12 .7Z" />
    </svg>
  );
}

function App() {
  const [menuOpen, setMenuOpen] = useState(false);
  const navItems = [
    ["Features", "#features"],
    ["Preview", "#preview"],
    ["Technology", "#technology"],
    ["Download", "#download"],
    ["FAQ", "#faq"],
  ];

  return (
    <div className="site-shell" id="top">
      <a className="skip-link" href="#main">Skip to content</a>
      <header className="site-header">
        <div className="header-inner">
          <a className="site-brand" href="#top" aria-label="Fasty Browser home"><BrandMark /><span>Fasty <small>Browser</small></span></a>
          <nav className="desktop-nav" aria-label="Primary navigation">
            {navItems.map(([label, href]) => <a href={href} key={href}>{label}</a>)}
          </nav>
          <div className="header-actions">
            <a className="github-link" href={REPO_URL} target="_blank" rel="noreferrer" aria-label="FastyBrowser on GitHub"><GithubIcon /></a>
            <a className="button button-primary button-small" href="#download">Get Fasty</a>
            <button className="menu-button" aria-label={menuOpen ? "Close navigation" : "Open navigation"} aria-expanded={menuOpen} onClick={() => setMenuOpen((value) => !value)}><TreeIcon id={menuOpen ? "close-circle" : "menu"} size={21} /></button>
          </div>
        </div>
        {menuOpen ? (
          <nav className="mobile-nav" aria-label="Mobile navigation">
            {navItems.map(([label, href]) => <a href={href} key={href} onClick={() => setMenuOpen(false)}>{label}</a>)}
            <a href={REPO_URL} target="_blank" rel="noreferrer">GitHub repository</a>
          </nav>
        ) : null}
      </header>

      <main id="main">
        <section className="hero-section" aria-labelledby="hero-title">
          <div className="container">
            <div className="hero-grid">
              <div className="hero-copy">
                <h1 id="hero-title">Less noise.<br /><em>More web.</em></h1>
                <p className="hero-description">Fasty is a private, native browser for Linux, with a Windows preview, built to stay out of your way—with sleeping tabs, a built-in shield, and local-first tools.</p>
                <div className="hero-actions">
                  <a className="button button-primary" href="#download"><TreeIcon id="direct-down" size={18} color="#ffffff" dark />Download for Linux</a>
                  <a className="button button-secondary" href="#download-windows"><TreeIcon id="direct-down" size={18} color="#5301fe" />Windows preview</a>
                  <a className="button button-secondary" href="#preview"><TreeIcon id="play" size={17} color="#5301fe" />Explore the preview</a>
                </div>
                <div className="hero-trust"><span><TreeIcon id="check" size={16} color="#0a9809" />No account</span><span><TreeIcon id="check" size={16} color="#0a9809" />No telemetry</span><span><TreeIcon id="check" size={16} color="#0a9809" />MIT licensed</span><span><TreeIcon id="check" size={16} color="#0a9809" />Linux x86_64</span><span><TreeIcon id="check" size={16} color="#0a9809" />Windows 10/11 preview</span></div>
              </div>
              <div className="hero-visual"><HeroBrowser /><div className="hero-caption"><span>A closer look at the Fasty experience</span><span>WebKitGTK 6.0 · GTK 4</span></div></div>
            </div>
            <div className="hero-proof" aria-label="Product highlights">
              <div><TreeIcon id="global" size={22} /><div><strong>Made for Linux</strong><span>Native desktop experience</span></div></div>
              <div><TreeIcon id="cpu" size={22} /><div><strong>WebKitGTK 6.0</strong><span>No Chromium or Electron</span></div></div>
              <div><TreeIcon id="eye-slash" size={22} /><div><strong>Local first</strong><span>Your data stays with you</span></div></div>
              <div><TreeIcon id="code" size={22} /><div><strong>Open source</strong><span>Inspect, build, and contribute</span></div></div>
            </div>
          </div>
        </section>

        <section className="section features-section" id="features" aria-labelledby="features-title">
          <div className="container">
            <SectionHeading id="features-title" title="A browser that gives you room." description="The essentials are calm and native. The advanced tools are there when you need them, with clear boundaries around experimental functionality." />
            <div className="feature-grid">
              {features.map((feature) => (
                <article className="feature-card" key={feature.title}>
                  <div className="card-head">
                    <TreeIcon id={feature.icon} size={25} color={feature.color} mode="hover" />
                    <h3>{feature.title}</h3>
                  </div>
                  <p>{feature.description}</p>
                  <FeatureStatus experimental={feature.experimental}>{feature.status}</FeatureStatus>
                </article>
              ))}
            </div>
          </div>
        </section>

        <section className="section preview-section" id="preview" aria-labelledby="preview-title">
          <div className="container">
            <SectionHeading dark id="preview-title" title="Meet the quieter browser." description="Choose a scene to explore the interface and product ideas behind Fasty." />
            <ProductPreview />
          </div>
        </section>

        <section className="section demos-section" aria-labelledby="demos-title">
          <div className="container">
            <SectionHeading id="demos-title" title="Small details. Real difference." description="Try the concepts behind the feature list. These are interactive explanations, not live browser sessions or performance benchmarks." />
            <div className="demo-grid"><SleepDemo /><ShieldDemo /><PipDemo /><AuthenticatorDemo /></div>
          </div>
        </section>

        <section className="section technology-section" id="technology" aria-labelledby="technology-title">
          <div className="container technology-grid">
            <div className="technology-copy">
              <SectionHeading id="technology-title" title="Native where it matters." description="Fasty is written in Go and uses the Linux platform’s own browser and interface stack. That means a focused application instead of a bundled web runtime." />
              <p>WebKitGTK handles the web platform. GTK 4 and libadwaita shape the desktop experience. Fasty’s own code manages tabs, privacy controls, extensions, local tools, and settings.</p>
              <div className="technology-points">
                <div><TreeIcon id="check" size={17} color="#0a9809" /><span>No account or remote service is required.</span></div>
                <div><TreeIcon id="check" size={17} color="#0a9809" /><span>Blocking and browser data are designed to remain local.</span></div>
                <div><TreeIcon id="check" size={17} color="#0a9809" /><span>The source is available under the MIT License.</span></div>
              </div>
              <TextLink href={REPO_URL} external>Explore the source</TextLink>            </div>
            <div className="stack-card">
              <div className="stack-card-header"><strong>The Fasty stack</strong><span>Linux desktop</span></div>
              <div className="stack-flow">
                <div className="stack-node"><TreeIcon id="code" size={23} /><strong>Go application</strong><span>Tabs, settings, privacy, extensions, and local tools</span></div>
                <div className="stack-node"><TreeIcon id="component" size={23} /><strong>GTK 4 + libadwaita</strong><span>Native controls and adaptive desktop UI</span></div>
                <div className="stack-node"><TreeIcon id="global" size={23} /><strong>WebKitGTK 6.0</strong><span>Web rendering and web-platform APIs</span></div>
                <div className="stack-node"><TreeIcon id="cpu" size={23} /><strong>Linux system</strong><span>Your distribution, packages, and desktop session</span></div>
              </div>
              <div className="stack-note"><TreeIcon id="information" size={16} /><span>Fasty does not bundle Chromium or Electron.</span></div>
            </div>
          </div>
        </section>

        <section className="section download-section" id="download" aria-labelledby="download-title">
          <div className="container download-grid">
            <div className="download-copy">
              <SectionHeading dark id="download-title" title="Fasty 1.1.1 is ready for Linux." description="Choose the package that fits your system. Every asset below comes from the official GitHub release and has a published SHA-256 checksum." />
              <p>Release assets target x86_64 Linux with glibc 2.39+. The documented baseline includes current Arch, Ubuntu 24.04+, Debian 13+, and Fedora 40+; release testing was reported on Arch only.</p>
              <div className="download-availability"><span />Four verified packages · x86_64 · Linux</div>
            </div>
            <DownloadPanel />
          </div>
        </section>

        <section className="section download-section" id="download-windows" aria-labelledby="download-windows-title">
          <div className="container download-grid">
            <div className="download-copy">
              <SectionHeading dark id="download-windows-title" title="Fasty for Windows is in preview." description="An installer and a portable zip for Windows 10 and 11. Both come from the official GitHub release and have a published SHA-256 checksum." />
              <p>The Windows build is written in Go and draws pages with Microsoft Edge WebView2, so it needs the WebView2 Runtime (already part of Windows 11 and current Windows 10). It has the tabs, shield, sleeping tabs and settings; it does not have passkeys, extensions or bookmarks yet. It was tested on Windows 11 only.</p>
              <div className="download-availability"><span />Two packages · x64 · Windows 10/11 · Preview</div>
              <p><a href="#download">Looking for Linux? Jump to the Linux downloads.</a></p>
            </div>
            <WindowsDownloadPanel />
          </div>
        </section>

        <section className="section about-section" id="about" aria-labelledby="about-title">
          <div className="container about-grid">
            <div className="about-logo-card"><BrandMark size={104} /><strong>FastHand Studio</strong><span>Original Fasty identity</span></div>
            <div className="about-copy">
              <SectionHeading id="about-title" title="Built in the open." description="FastyBrowser is an independent open-source project from FastHand Studio, exploring a calmer native browser for Linux." />
              <p>The project combines Go, WebKitGTK, GTK, and libadwaita with a local-first approach to privacy and browser data. It is a young project: site compatibility, extension APIs, passkeys, and PiP have documented limits. Its original logo and brand assets are used here under the repository’s MIT License.</p>
              <div className="about-facts"><div><small>Author</small><strong>FastHand Studio</strong></div><div><small>License</small><strong>MIT</strong></div><div><small>Language</small><strong>Go</strong></div><div><small>Engine</small><strong>WebKitGTK 6.0</strong></div></div>
              <div className="about-links"><TextLink href={REPO_URL} external>GitHub repository</TextLink><TextLink href={publicUrl("/LICENSE.fasty.txt")} external>Read the license</TextLink><TextLink href={publicUrl("/fasty-logo.svg")} external>Download original logo</TextLink></div>
            </div>
          </div>
        </section>

        <section className="section docs-section" id="docs" aria-labelledby="docs-title">
          <div className="container">
            <SectionHeading id="docs-title" title="Know what you are installing." description="Use the project guide and official repository for accurate, current instructions. The website does not replace the source documentation." />
            <div className="docs-grid">
              <a className="doc-card" href={GUIDE_URL} download><div className="card-head"><TreeIcon id="book" size={25} /><h3>Product guide</h3></div><p>Overview, platform availability, installation preparation, privacy, and project information in one PDF.</p><span>Download PDF <TreeIcon id="direct-down" size={16} /></span></a>
              <a className="doc-card" href={INSTALL_DOC_URL} target="_blank" rel="noreferrer"><div className="card-head"><TreeIcon id="code" size={25} /><h3>Installation docs</h3></div><p>Read the current guide for dependencies, source builds, release installation, and platform notes.</p><span>Open install guide <TreeIcon id="arrow-right" size={16} /></span></a>
              <a className="doc-card" href="#technology"><div className="card-head"><TreeIcon id="shield-security" size={25} /><h3>Privacy model</h3></div><p>Understand the account-free, telemetry-free, local-first design described by the project.</p><span>Explore technology <TreeIcon id="arrow-right" size={16} /></span></a>
              <a className="doc-card" href="#preview"><div className="card-head"><TreeIcon id="lock" size={25} /><h3>Agent control</h3></div><p>Review the authorization boundary and experimental request-tooling preview.</p><span>View preview <TreeIcon id="arrow-right" size={16} /></span></a>
            </div>
          </div>
        </section>

        <section className="section faq-section" id="faq" aria-labelledby="faq-title">
          <div className="container faq-layout">
            <div>
              <SectionHeading id="faq-title" title="Questions, answered plainly." />
              <p>Need the latest technical detail? The repository README and issue tracker are the authoritative sources.</p>
              <div className="faq-layout-links">
                <TextLink href={REPO_URL} external>Open the project repository</TextLink><TextLink href={`${REPO_URL}/issues`} external>Report an issue</TextLink><TextLink href={FEATURES_DOC_URL} external>Feature limitations</TextLink>
              </div>
            </div>
            <div className="faq-list">
              {faqs.map((faq, index) => (
                <details key={faq.question} open={index === 0}>
                  <summary>{faq.question}<TreeIcon id="arrow-right" size={18} className="faq-mark" /></summary>
                  <p>{faq.answer}</p>
                </details>
              ))}
            </div>
          </div>
        </section>

        <section className="final-cta" aria-labelledby="final-cta-title">
          <div className="container final-cta-inner">
            <div><h2 id="final-cta-title">A quieter browser is worth a closer look.</h2><p>Explore the source, read the guide, and see whether Fasty fits your Linux desktop.</p></div>
            <div className="final-cta-actions"><a className="button button-on-dark" href="#download"><TreeIcon id="direct-down" size={18} color="#171717" />Get Fasty</a><a className="text-link" href={GUIDE_URL} download>Download product guide</a></div>
          </div>
        </section>
      </main>

      <footer className="site-footer">
        <div className="container">
          <div className="footer-top">
            <div className="footer-brand"><a className="site-brand" href="#top"><BrandMark /><span>Fasty <small>Browser</small></span></a><p>A native, private Linux browser from FastHand Studio. This website is a product presentation, not the browser itself.</p></div>
            <div className="footer-column"><strong>Product</strong><a href="#features">Features</a><a href="#preview">Interactive preview</a><a href="#download">Download & install</a></div>
            <div className="footer-column"><strong>Project</strong><a href="#about">About</a><a href={STUDIO_URL}>FastHand Studio</a><a href="#technology">Technology</a><a href={REPO_URL} target="_blank" rel="noreferrer">Source code</a></div>
            <div className="footer-column"><strong>Resources</strong><a href={GUIDE_URL} download>Product guide</a><a href="#docs">Documentation</a><a href="#faq">FAQ</a></div>
          </div>
          <div className="footer-bottom"><span>Fasty logo and brand assets © 2026 FastHand Studio. Used under the MIT License.</span><span>Website preview · No account · No telemetry</span></div>
        </div>
      </footer>
    </div>
  );
}

export default App;
