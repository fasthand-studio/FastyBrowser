import type { ComponentType, ReactNode } from "react";
import type { IconId, IconProps } from "icon-tree-animate";
import AddIcon from "icon-tree-animate/icons/add";
import ArrowRightIcon from "icon-tree-animate/icons/arrow-right";
import BookIcon from "icon-tree-animate/icons/book";
import CheckIcon from "icon-tree-animate/icons/check";
import CloseCircleIcon from "icon-tree-animate/icons/close-circle";
import CodeIcon from "icon-tree-animate/icons/code";
import ComponentIcon from "icon-tree-animate/icons/component";
import CopyIcon from "icon-tree-animate/icons/copy";
import CpuIcon from "icon-tree-animate/icons/cpu";
import DirectDownIcon from "icon-tree-animate/icons/direct-down";
import EyeSlashIcon from "icon-tree-animate/icons/eye-slash";
import FingerScanIcon from "icon-tree-animate/icons/finger-scan";
import GlobalIcon from "icon-tree-animate/icons/global";
import InformationIcon from "icon-tree-animate/icons/information";
import LinkIcon from "icon-tree-animate/icons/link";
import LockIcon from "icon-tree-animate/icons/lock";
import MenuIcon from "icon-tree-animate/icons/menu";
import MoonIcon from "icon-tree-animate/icons/moon";
import MoreIcon from "icon-tree-animate/icons/more";
import NoteIcon from "icon-tree-animate/icons/note";
import PauseIcon from "icon-tree-animate/icons/pause";
import PlayIcon from "icon-tree-animate/icons/play";
import RefreshIcon from "icon-tree-animate/icons/refresh";
import RoutingIcon from "icon-tree-animate/icons/routing";
import SearchNormalIcon from "icon-tree-animate/icons/search-normal";
import ShieldSecurityIcon from "icon-tree-animate/icons/shield-security";
import ShieldTickIcon from "icon-tree-animate/icons/shield-tick";
import TimerIcon from "icon-tree-animate/icons/timer";
import VideoPlayIcon from "icon-tree-animate/icons/video-play";

type PerIconProps = Omit<IconProps, "id" | "loadAnimation">;

const ICONS: Partial<Record<IconId, ComponentType<PerIconProps>>> = {
  add: AddIcon,
  "arrow-right": ArrowRightIcon,
  book: BookIcon,
  check: CheckIcon,
  "close-circle": CloseCircleIcon,
  code: CodeIcon,
  component: ComponentIcon,
  copy: CopyIcon,
  cpu: CpuIcon,
  "direct-down": DirectDownIcon,
  "eye-slash": EyeSlashIcon,
  "finger-scan": FingerScanIcon,
  global: GlobalIcon,
  information: InformationIcon,
  link: LinkIcon,
  lock: LockIcon,
  menu: MenuIcon,
  moon: MoonIcon,
  more: MoreIcon,
  note: NoteIcon,
  pause: PauseIcon,
  play: PlayIcon,
  refresh: RefreshIcon,
  routing: RoutingIcon,
  "search-normal": SearchNormalIcon,
  "shield-security": ShieldSecurityIcon,
  "shield-tick": ShieldTickIcon,
  timer: TimerIcon,
  "video-play": VideoPlayIcon,
};

export function TreeIcon({
  id,
  size = 20,
  color,
  dark = false,
  mode = "static",
  className = "",
}: {
  id: IconId;
  size?: number;
  color?: string;
  dark?: boolean;
  mode?: PerIconProps["mode"];
  className?: string;
}) {
  const Component = ICONS[id];
  if (!Component) return null;
  const resolved = color ?? (dark ? "#d8d5cf" : "#6c6861");
  return (
    <span className={`tree-icon ${className}`} style={{ width: size, height: size, color: resolved }}>
      <Component key={`${id}-${dark}-${resolved}`} size={size} color={resolved} mode={mode} />
    </span>
  );
}

export function BrandMark({ size = 32, className = "" }: { size?: number; className?: string }) {
  return <img className={`brand-mark ${className}`} src="/fasty-logo.svg" alt="Fasty Browser logo" width={size} height={size} />;
}

export function SectionHeading({
  eyebrow,
  title,
  description,
  dark = false,
  align = "left",
  id,
}: {
  eyebrow: string;
  title: string;
  description?: string;
  dark?: boolean;
  align?: "left" | "center";
  id?: string;
}) {
  return (
    <div className={`section-heading ${dark ? "on-dark" : ""} ${align === "center" ? "is-centered" : ""}`}>
      <span className="section-eyebrow">{eyebrow}</span>
      <h2 id={id}>{title}</h2>
      {description ? <p>{description}</p> : null}
    </div>
  );
}

export function FeatureStatus({ children, experimental = false }: { children: ReactNode; experimental?: boolean }) {
  return (
    <span className={`feature-status ${experimental ? "is-experimental" : ""}`}>
      <span />{children}
    </span>
  );
}

export function TextLink({ href, children, external = false }: { href: string; children: ReactNode; external?: boolean }) {
  return (
    <a className="text-link" href={href} {...(external ? { target: "_blank", rel: "noreferrer" } : {})}>
      {children}<TreeIcon id="arrow-right" size={17} />
    </a>
  );
}

export function BrowserDots() {
  return (
    <span className="browser-dots" aria-hidden="true">
      <span /><span /><span />
    </span>
  );
}
