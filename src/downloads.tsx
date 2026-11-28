import { useState } from "react";
import { publicUrl, TreeIcon } from "./components";

const RELEASE_BASE = "https://github.com/fasthand-studio/FastyBrowser/releases/download/v1.1.1";

const packages = [
  {
    id: "appimage",
    label: "AppImage",
    name: "Fasty-1.1.1-x86_64.AppImage",
    size: "9.83 MB",
    href: `${RELEASE_BASE}/Fasty-1.1.1-x86_64.AppImage`,
    sha256: "6239358c0643f817b88189da2ac6cbd0c0239c8d93ae3a68f89a50eb99d841dc",
    command: "chmod +x Fasty-1.1.1-x86_64.AppImage\n./Fasty-1.1.1-x86_64.AppImage",
    note: "Requires WebKitGTK 6.0, GTK 4, and libadwaita on your system. The AppImage is not self-contained.",
  },
  {
    id: "deb",
    label: "Debian / Ubuntu",
    name: "fasty-browser_1.1.1_amd64.deb",
    size: "10.1 MB",
    href: `${RELEASE_BASE}/fasty-browser_1.1.1_amd64.deb`,
    sha256: "08aa2050e519f56ebb1edbc973e14acf9cd3263573749d346ec1b4acb80e0a6b",
    command: "sudo apt install ./fasty-browser_1.1.1_amd64.deb",
    note: "Documented baseline: glibc 2.39+, including Ubuntu 24.04+ and Debian 13+. Release testing was reported on Arch only.",
  },
  {
    id: "rpm",
    label: "Fedora / RPM",
    name: "fasty-browser-1.1.1-1.x86_64.rpm",
    size: "10.4 MB",
    href: `${RELEASE_BASE}/fasty-browser-1.1.1-1.x86_64.rpm`,
    sha256: "de52a87dd9be86afa2ab987dda17dc3ff9eef2719eecbd4ffbf8bd6e5d6afc5a",
    command: "sudo dnf install ./fasty-browser-1.1.1-1.x86_64.rpm",
    note: "Documented baseline: glibc 2.39+, including Fedora 40+. H.264 may require RPM Fusion and libavcodec-freeworld.",
  },
  {
    id: "tarball",
    label: "Tarball",
    name: "fasty-browser-1.1.1-linux-x86_64.tar.gz",
    size: "9.95 MB",
    href: `${RELEASE_BASE}/fasty-browser-1.1.1-linux-x86_64.tar.gz`,
    sha256: "f53ef8a507312fc76967c6cf4e2f871a70eaedfd594ea2560c69b5ab0c00842f",
    command: "tar -xzf fasty-browser-1.1.1-linux-x86_64.tar.gz\ncd fasty-browser-1.1.1-linux-x86_64\n./install.sh",
    note: "The release installer checks libraries and can install missing packages with elevation. It installs under your user account.",
  },
];

function GithubIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d="M12 .7a11.5 11.5 0 0 0-3.64 22.41c.58.1.79-.25.79-.56v-2.23c-3.22.7-3.9-1.37-3.9-1.37-.52-1.33-1.28-1.69-1.28-1.69-1.05-.72.08-.7.08-.7 1.16.08 1.77 1.19 1.77 1.19 1.03 1.77 2.7 1.26 3.36.96.1-.75.4-1.26.73-1.55-2.57-.29-5.27-1.29-5.27-5.69 0-1.26.45-2.28 1.19-3.09-.12-.29-.52-1.46.11-3.05 0 0 .97-.31 3.16 1.18a10.9 10.9 0 0 1 5.76 0c2.19-1.49 3.15-1.18 3.15-1.18.63 1.59.23 2.76.12 3.05.74.81 1.19 1.83 1.19 3.09 0 4.41-2.71 5.39-5.29 5.68.42.36.79 1.07.79 2.16v3.21c0 .31.21.67.8.56A11.5 11.5 0 0 0 12 .7Z" />
    </svg>
  );
}

export function DownloadPanel() {
  const [selectedId, setSelectedId] = useState("appimage");
  const [copyState, setCopyState] = useState<"idle" | "copied" | "error">("idle");
  const selected = packages.find((item) => item.id === selectedId) ?? packages[0];

  const copyCommand = async () => {
    try {
      await navigator.clipboard.writeText(selected.command);
      setCopyState("copied");
      window.setTimeout(() => setCopyState("idle"), 1800);
    } catch {
      setCopyState("error");
    }
  };

  return (
    <div className="download-card">
      <div className="download-card-top">
        <div className="platform-mark"><TreeIcon id="global" size={30} color="#0579fe" /></div>
        <div><strong>Fasty Browser 1.1.1</strong><span>Linux x86_64 · Released 24 Nov 2026</span></div>
        <div className="download-state is-available"><span />Verified release</div>
      </div>
      <div className="package-tabs" role="tablist" aria-label="Linux package format">
        {packages.map((item) => (
          <button key={item.id} id={`package-tab-${item.id}`} role="tab" aria-selected={selectedId === item.id} aria-controls={`package-panel-${item.id}`} className={selectedId === item.id ? "is-active" : ""} onClick={() => { setSelectedId(item.id); setCopyState("idle"); }}>{item.label}</button>
        ))}
      </div>
      <div className="package-panel" id={`package-panel-${selected.id}`} role="tabpanel" aria-labelledby={`package-tab-${selected.id}`}>
        <div className="package-file"><div><small>SELECTED ASSET</small><strong>{selected.name}</strong></div><span>{selected.size}</span></div>
        <a className="button button-on-dark package-download" href={selected.href} download><TreeIcon id="direct-down" size={18} color="#171717" />Download {selected.label}</a>
        <div className="install-command">
          <div><span>INSTALL</span><button onClick={copyCommand} aria-label="Copy installation command"><TreeIcon id={copyState === "copied" ? "check" : "copy"} size={15} color={copyState === "copied" ? "#74d174" : "#aaa59e"} dark />{copyState === "copied" ? "Copied" : copyState === "error" ? "Select manually" : "Copy"}</button></div>
          <pre>{selected.command}</pre>
        </div>
        <p className="package-note">{selected.note}</p>
        <details className="checksum-details">
          <summary>SHA-256 checksum</summary>
          <code>{selected.sha256}</code>
        </details>
      </div>
      <div className="download-requirements">
        <div><small>Architecture</small><strong>x86_64 only</strong></div>
        <div><small>Baseline</small><strong>glibc 2.39+</strong></div>
        <div><small>Engine</small><strong>WebKitGTK 6.0</strong></div>
        <div><small>Interface</small><strong>GTK 4 + libadwaita</strong></div>
      </div>
      <div className="download-notice"><TreeIcon id="information" size={18} color="#d9b973" dark /><span>Documented Linux baseline: current Arch, Ubuntu 24.04+, Debian 13+, and Fedora 40+. Release notes report testing on Arch only. No Windows, macOS, or ARM64 asset is published.</span></div>
      <div className="download-secondary-links"><a href={`${RELEASE_BASE}/SHA256SUMS`} target="_blank" rel="noreferrer"><TreeIcon id="shield-security" size={16} color="#aaa59e" dark />Checksums</a><a href="https://github.com/fasthand-studio/FastyBrowser/releases/tag/v1.1.1" target="_blank" rel="noreferrer"><GithubIcon />Release notes</a><a href="https://github.com/fasthand-studio/FastyBrowser/blob/main/docs/install.md" target="_blank" rel="noreferrer"><TreeIcon id="code" size={16} color="#aaa59e" dark />Install guide</a><a href={publicUrl("/fasty-product-guide.pdf")} download><TreeIcon id="book" size={16} color="#aaa59e" dark />Product guide</a></div>
    </div>
  );
}
