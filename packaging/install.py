#!/usr/bin/env python3
"""Fasty Browser installer.

Run ./install.sh from the unpacked release. It installs for your user only (no
root), then asks a few questions: make Fasty your default browser, which search
engine to use, whether the libraries Fasty needs are all there, and whether to
bring over bookmarks and history from another browser.

It can also run without asking, for scripts:

  ./install.sh --yes --default-browser no --engine duckduckgo --import none
"""
import argparse
import glob
import importlib.util
import json
import os
import platform
import re
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import time

HERE = os.path.dirname(os.path.abspath(__file__))
APP_ID = "app.fastybrowser.Linux"
DESKTOP = APP_ID + ".desktop"

ENGINES = [  # key, name  (the same keys the browser uses)
    ("duckduckgo", "DuckDuckGo"), ("google", "Google"), ("bing", "Bing"),
    ("ecosia", "Ecosia"), ("startpage", "Startpage"), ("kagi", "Kagi"),
    ("brave", "Brave Search"), ("qwant", "Qwant"), ("custom", "Custom address"),
]

IS_TTY = sys.stdin.isatty() and sys.stdout.isatty()


# ---- asking -------------------------------------------------------------------

class Ask:
    """Questions, or the default answer when running without a terminal."""

    def __init__(self, auto):
        self.auto = auto or not IS_TTY

    def yes_no(self, question, default=True):
        if self.auto:
            return default
        hint = "Y/n" if default else "y/N"
        while True:
            a = input(f"{question} [{hint}] ").strip().lower()
            if a == "":
                return default
            if a in ("y", "yes"):
                return True
            if a in ("n", "no"):
                return False

    def choose(self, question, options, default=0):
        """options: list of labels. Returns the index chosen."""
        if self.auto:
            return default
        print(question)
        for i, o in enumerate(options, 1):
            mark = "  (default)" if i - 1 == default else ""
            print(f"  {i}) {o}{mark}")
        while True:
            a = input(f"Number [{default + 1}]: ").strip()
            if a == "":
                return default
            if a.isdigit() and 1 <= int(a) <= len(options):
                return int(a) - 1

    def many(self, question, options):
        """Pick any of options; returns a list of indexes (none = skip)."""
        if self.auto:
            return []
        print(question)
        for i, o in enumerate(options, 1):
            print(f"  {i}) {o}")
        while True:
            a = input("Numbers separated by spaces, or Enter to skip: ").strip()
            if a == "":
                return []
            try:
                picks = sorted({int(x) - 1 for x in a.replace(",", " ").split()})
            except ValueError:
                continue
            if all(0 <= p < len(options) for p in picks):
                return picks

    def text(self, question, default=""):
        if self.auto:
            return default
        a = input(f"{question} " + (f"[{default}] " if default else "")).strip()
        return a or default


def say(msg=""):
    print(msg, flush=True)


def step(msg):
    say("\n== " + msg)


# ---- what Fasty needs -------------------------------------------------------------

MIN_GLIBC = (2, 39)  # what this build was linked against


def distro():
    """arch, debian (Debian, Ubuntu and their relatives) or fedora; "" if unknown."""
    try:
        with open("/etc/os-release") as f:
            d = {}
            for line in f:
                if "=" in line:
                    k, v = line.strip().split("=", 1)
                    d[k] = v.strip('"')
    except OSError:
        return ""
    ids = (d.get("ID", "") + " " + d.get("ID_LIKE", "")).lower().split()
    for names, key in ((("arch", "cachyos", "manjaro", "endeavouros"), "arch"),
                       (("debian", "ubuntu", "linuxmint", "pop", "elementary"), "debian"),
                       (("fedora", "rhel", "centos", "nobara"), "fedora")):
        if any(n in ids for n in names):
            return key
    return ""


def distro_name():
    try:
        with open("/etc/os-release") as f:
            for line in f:
                if line.startswith("PRETTY_NAME="):
                    return line.split("=", 1)[1].strip().strip('"')
    except OSError:
        pass
    return "Linux"


def glibc_version():
    try:
        return tuple(int(x) for x in os.confstr("CS_GNU_LIBC_VERSION").split()[1].split(".")[:2])
    except (OSError, ValueError, IndexError):
        return None


# Libraries Fasty links against, and the package that has them on each system.
# Installing the first three brings in the rest as dependencies.
PACKAGES = {
    "libwebkitgtk-6.0.so.4": {"arch": "webkitgtk-6.0", "debian": "libwebkitgtk-6.0-4", "fedora": "webkitgtk6.0"},
    "libjavascriptcoregtk-6.0.so.1": {"arch": "webkitgtk-6.0", "debian": "libjavascriptcoregtk-6.0-1", "fedora": "webkitgtk6.0"},
    "libgtk-4.so.1": {"arch": "gtk4", "debian": "libgtk-4-1", "fedora": "gtk4"},
    "libadwaita-1.so.0": {"arch": "libadwaita", "debian": "libadwaita-1-0", "fedora": "libadwaita"},
    "libsoup-3.0.so.0": {"arch": "libsoup3", "debian": "libsoup-3.0-0", "fedora": "libsoup3"},
    "libgirepository-1.0.so.1": {"arch": "gobject-introspection-runtime", "debian": "libgirepository-1.0-1", "fedora": "gobject-introspection"},
    "libgraphene-1.0.so.0": {"arch": "graphene", "debian": "libgraphene-1.0-0", "fedora": "graphene"},
    "libvulkan.so.1": {"arch": "vulkan-icd-loader", "debian": "libvulkan1", "fedora": "vulkan-loader"},
}
MEDIA = {  # video and audio playback
    "arch": ["gst-plugins-good", "gst-plugins-bad", "gst-libav"],
    "debian": ["gstreamer1.0-plugins-good", "gstreamer1.0-plugins-bad", "gstreamer1.0-libav"],
    "fedora": ["gstreamer1-plugins-good", "gstreamer1-plugins-bad-free", "gstreamer1-plugin-libav"],
}
MEDIA_NOTE = {
    "fedora": "Fedora leaves H.264 out of its own packages. For YouTube and most video, enable RPM Fusion "
              "and install libavcodec-freeworld.",
}
KEYRING = {"arch": "libsecret", "debian": "libsecret-tools", "fedora": "libsecret"}
# what the graphical installer itself needs
GUI_PACKAGES = {
    "arch": ["python-gobject", "libadwaita"],
    "debian": ["python3-gi", "gir1.2-gtk-4.0", "gir1.2-adw-1"],
    "fedora": ["python3-gobject", "libadwaita"],
}
INSTALL_ARGV = {  # needs root
    "arch": ["pacman", "-S", "--needed", "--noconfirm"],
    "debian": ["apt-get", "install", "-y"],
    "fedora": ["dnf", "install", "-y"],
}


def install_command(dist, pkgs, use_sudo=True):
    argv = (["sudo"] if use_sudo else []) + INSTALL_ARGV[dist] + list(pkgs)
    if dist == "debian" and use_sudo:
        argv = ["sudo", "apt", "install"] + list(pkgs)
    return argv


def missing_libraries(binary):
    try:
        out = subprocess.run(["ldd", binary], capture_output=True, text=True, timeout=30).stdout
    except (OSError, subprocess.TimeoutExpired):
        return []
    return [l.split()[0] for l in out.splitlines() if "not found" in l]


def have_gst_element(name):
    if not shutil.which("gst-inspect-1.0"):
        return None
    return subprocess.run(["gst-inspect-1.0", "--exists", name], capture_output=True).returncode == 0


class Needs:
    """What is missing on this computer."""

    def __init__(self, binary):
        self.dist = distro()
        self.name = distro_name()
        self.problems = []   # things that stop Fasty from starting: (what, package or None)
        self.media = []      # packages for playing video
        self.note = ""
        self.keyring = None
        g = glibc_version()
        self.too_old = bool(g and g < MIN_GLIBC)
        if self.too_old:
            self.problems.append((f"a newer system (this build needs glibc {MIN_GLIBC[0]}.{MIN_GLIBC[1]} or later; "
                                  f"this one has {g[0]}.{g[1]})", None))
        for lib in missing_libraries(binary):
            self.problems.append((lib, PACKAGES.get(lib, {}).get(self.dist)))
        if have_gst_element("avdec_h264") is False or have_gst_element("vp9dec") is False or \
                (have_gst_element("avdec_h264") is None and not glob.glob("/usr/lib*/**/libgstlibav.so", recursive=True)):
            self.media = list(MEDIA.get(self.dist, []))
            self.note = MEDIA_NOTE.get(self.dist, "")
        if not shutil.which("secret-tool") and self.dist in KEYRING:
            self.keyring = KEYRING[self.dist]

    @property
    def packages(self):
        pk = {p for _, p in self.problems if p} | set(self.media)
        if self.keyring:
            pk.add(self.keyring)
        return sorted(pk)

    def argv(self, root=None):
        """The command that installs everything missing in one go. root: ["sudo"], ["pkexec"], or [] when already root."""
        root = ["sudo"] if root is None else root
        pk = self.packages
        if self.dist == "fedora" and self.media:
            # Fedora ships no H.264. RPM Fusion has it, so turn that on too: one step, one password.
            script = ("dnf install -y https://mirrors.rpmfusion.org/free/fedora/rpmfusion-free-release-$(rpm -E %fedora).noarch.rpm"
                      " && dnf install -y " + " ".join(pk) + " libavcodec-freeworld")
            return root + ["sh", "-c", script]
        return root + INSTALL_ARGV[self.dist] + pk

    @property
    def ok(self):
        return not self.problems


def check_requirements(binary, ask, skip):
    step("Checking what Fasty needs")
    n = Needs(binary)
    say(f"System: {n.name}" + ("" if n.dist else "  (not one Fasty knows by name; package names below may not apply)"))
    if n.ok and not n.media:
        say("Everything Fasty needs is here.")
    for what, pkg in n.problems:
        say(f"  missing: {what}" + (f"  ({pkg})" if pkg else ""))
    if n.media:
        say("  missing: video and audio playback (" + ", ".join(n.media) + ")")
    if n.note:
        say("  " + n.note)
    pkgs = n.packages
    if pkgs and n.dist in INSTALL_ARGV:
        say("\nTo install them:  " + " ".join(n.argv()))
        if not skip and ask.yes_no("Install them now? (it will ask for your password)", default=True):
            subprocess.run(n.argv())
            n = Needs(binary)
    elif n.problems and not pkgs:
        say("Install the missing libraries with your package manager, then run this again.")
    return n.ok


# ---- installing the files ---------------------------------------------------------------

def install_files(prefix):
    step(f"Installing to {prefix}")
    appdir = os.path.join(prefix, "lib", "fasty-browser")
    os.makedirs(appdir, exist_ok=True)
    for d in ("bin", "share/applications", "share/icons/hicolor/scalable/apps"):
        os.makedirs(os.path.join(prefix, d), exist_ok=True)
    updating = os.path.exists(os.path.join(appdir, "fasty-browser-bin"))
    for name, mode in (("fasty-browser-bin", 0o755), ("fasty-browser", 0o755), ("libfasty-uvm.so", 0o755)):
        src = os.path.join(HERE, name)
        if os.path.exists(src):
            tmp = os.path.join(appdir, name + ".new")
            shutil.copyfile(src, tmp)
            os.chmod(tmp, mode)
            os.replace(tmp, os.path.join(appdir, name))  # safe while it is running
    link = os.path.join(prefix, "bin", "fasty-browser")
    if os.path.lexists(link):
        os.remove(link)
    os.symlink(os.path.join(appdir, "fasty-browser"), link)
    shutil.copyfile(os.path.join(HERE, "fasty-browser.svg"),
                    os.path.join(prefix, "share/icons/hicolor/scalable/apps/fasty-browser.svg"))
    with open(os.path.join(HERE, DESKTOP)) as f:
        text = f.read().replace("Exec=fasty-browser", "Exec=" + link)
    with open(os.path.join(prefix, "share/applications", DESKTOP), "w") as f:
        f.write(text)
    for cmd in (["update-desktop-database", os.path.join(prefix, "share/applications")],
                ["gtk-update-icon-cache", "-q", os.path.join(prefix, "share/icons/hicolor")]):
        if shutil.which(cmd[0]):
            subprocess.run(cmd, capture_output=True)
    say("Updated Fasty." if updating else "Installed Fasty.")
    return link


def uninstall(prefix):
    step(f"Removing Fasty from {prefix}")
    for path in (os.path.join(prefix, "lib", "fasty-browser"), os.path.join(prefix, "bin", "fasty-browser"),
                 os.path.join(prefix, "share/applications", DESKTOP),
                 os.path.join(prefix, "share/icons/hicolor/scalable/apps/fasty-browser.svg")):
        if os.path.isdir(path) and not os.path.islink(path):
            shutil.rmtree(path, ignore_errors=True)
        elif os.path.lexists(path):
            os.remove(path)
    if shutil.which("update-desktop-database"):
        subprocess.run(["update-desktop-database", os.path.join(prefix, "share/applications")], capture_output=True)
    say("Removed. Your history, bookmarks and settings are still in ~/.local/share/fasty-browser.")


# ---- settings ------------------------------------------------------------------------------

def data_dir():
    base = os.environ.get("XDG_DATA_HOME") or os.path.expanduser("~/.local/share")
    d = os.path.join(base, "fasty-browser")
    os.makedirs(d, mode=0o700, exist_ok=True)
    os.chmod(d, 0o700)
    return d


def read_json(path, default):
    try:
        with open(path) as f:
            return json.load(f)
    except (OSError, ValueError):
        return default


def write_json(path, value):
    tmp = path + ".tmp"
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as f:
        json.dump(value, f)
    os.replace(tmp, path)


def set_settings(**kv):
    path = os.path.join(data_dir(), "settings.json")
    s = read_json(path, {})
    s.update(kv)
    write_json(path, s)


# ---- default browser --------------------------------------------------------------------------

def set_default_browser():
    if not shutil.which("xdg-settings"):
        say("Couldn't set the default browser (xdg-settings isn't installed). Use Settings > General in Fasty.")
        return False
    subprocess.run(["xdg-settings", "set", "default-web-browser", DESKTOP], capture_output=True)
    for mime in ("x-scheme-handler/http", "x-scheme-handler/https"):
        if shutil.which("xdg-mime"):
            subprocess.run(["xdg-mime", "default", DESKTOP, mime], capture_output=True)
    now = subprocess.run(["xdg-settings", "get", "default-web-browser"], capture_output=True, text=True).stdout.strip()
    ok = now == DESKTOP
    say("Fasty is now your default browser." if ok else
        f"Couldn't make Fasty the default (it is still {now or 'unset'}). Use Settings > General in Fasty.")
    return ok


# ---- importing from other browsers ----------------------------------------------------------------

FIREFOX_ROOTS = {
    "Firefox": ["~/.mozilla/firefox", "~/.var/app/org.mozilla.firefox/.mozilla/firefox",
                "~/snap/firefox/common/.mozilla/firefox"],
    "Zen": ["~/.zen", "~/.var/app/app.zen_browser.zen/.zen"],
    "LibreWolf": ["~/.librewolf", "~/.var/app/io.gitlab.librewolf-community/.librewolf"],
    "Waterfox": ["~/.waterfox"],
}
CHROMIUM_ROOTS = {
    "Chromium": ["~/.config/chromium", "~/.var/app/org.chromium.Chromium/config/chromium"],
    "Google Chrome": ["~/.config/google-chrome", "~/.var/app/com.google.Chrome/config/google-chrome"],
    "Brave": ["~/.config/BraveSoftware/Brave-Browser", "~/.var/app/com.brave.Browser/config/BraveSoftware/Brave-Browser"],
    "Vivaldi": ["~/.config/vivaldi"],
    "Microsoft Edge": ["~/.config/microsoft-edge"],
    "Opera": ["~/.config/opera"],
}


class Source:
    def __init__(self, browser, kind, path, label):
        self.browser, self.kind, self.path, self.label = browser, kind, path, label  # kind: firefox | chromium


def find_sources():
    found = []
    for name, roots in FIREFOX_ROOTS.items():
        for root in roots:
            for db in sorted(glob.glob(os.path.join(os.path.expanduser(root), "*", "places.sqlite"))):
                prof = os.path.basename(os.path.dirname(db))
                found.append(Source(name, "firefox", os.path.dirname(db), f"{name} ({prof})"))
    for name, roots in CHROMIUM_ROOTS.items():
        for root in roots:
            root = os.path.expanduser(root)
            for prof in ["Default"] + sorted(os.path.basename(p) for p in glob.glob(os.path.join(root, "Profile *"))):
                p = os.path.join(root, prof)
                if os.path.exists(os.path.join(p, "Bookmarks")) or os.path.exists(os.path.join(p, "History")):
                    found.append(Source(name, "chromium", p, f"{name} ({prof})"))
    return found


def copy_db(path):
    """A readable copy of a database the other browser may have open."""
    tmp = tempfile.mkdtemp(prefix="fasty-import-")
    dst = os.path.join(tmp, os.path.basename(path))
    shutil.copyfile(path, dst)
    for ext in ("-wal", "-shm"):
        if os.path.exists(path + ext):
            shutil.copyfile(path + ext, dst + ext)
    return dst, tmp


def web(url):
    return isinstance(url, str) and (url.startswith("http://") or url.startswith("https://"))


def firefox_bookmarks(src):
    db = os.path.join(src.path, "places.sqlite")
    if not os.path.exists(db):
        return []
    dst, tmp = copy_db(db)
    try:
        con = sqlite3.connect(f"file:{dst}?mode=ro", uri=True)
        rows = con.execute(
            "SELECT p.url, COALESCE(NULLIF(b.title,''), p.title, ''), b.dateAdded FROM moz_bookmarks b "
            "JOIN moz_places p ON p.id = b.fk WHERE b.type = 1").fetchall()
        con.close()
    except sqlite3.Error:
        rows = []
    finally:
        shutil.rmtree(tmp, ignore_errors=True)
    return [{"url": u, "title": t or u, "date": (d or 0) / 1e6} for u, t, d in rows if web(u)]


def firefox_history(src, limit):
    db = os.path.join(src.path, "places.sqlite")
    if not os.path.exists(db):
        return []
    dst, tmp = copy_db(db)
    try:
        con = sqlite3.connect(f"file:{dst}?mode=ro", uri=True)
        rows = con.execute(
            "SELECT url, COALESCE(title,''), visit_count, COALESCE(last_visit_date,0) FROM moz_places "
            "WHERE visit_count > 0 ORDER BY last_visit_date DESC LIMIT ?", (limit * 2,)).fetchall()
        con.close()
    except sqlite3.Error:
        rows = []
    finally:
        shutil.rmtree(tmp, ignore_errors=True)
    out = [{"url": u, "title": t, "visits": int(v), "last": l / 1e6} for u, t, v, l in rows if web(u)]
    return out[:limit]


def chromium_time(us):
    """Chromium counts microseconds from 1601-01-01."""
    return (int(us) / 1e6 - 11644473600) if us else 0


def chromium_bookmarks(src):
    path = os.path.join(src.path, "Bookmarks")
    data = read_json(path, {})
    out = []

    def walk(node):
        if not isinstance(node, dict):
            return
        if node.get("type") == "url" and web(node.get("url")):
            out.append({"url": node["url"], "title": node.get("name") or node["url"],
                        "date": chromium_time(node.get("date_added", 0))})
        for c in node.get("children", []) or []:
            walk(c)

    for root in (data.get("roots") or {}).values():
        walk(root)
    return out


def chromium_history(src, limit):
    db = os.path.join(src.path, "History")
    if not os.path.exists(db):
        return []
    dst, tmp = copy_db(db)
    try:
        con = sqlite3.connect(f"file:{dst}?mode=ro", uri=True)
        rows = con.execute(
            "SELECT url, COALESCE(title,''), visit_count, last_visit_time FROM urls "
            "WHERE visit_count > 0 ORDER BY last_visit_time DESC LIMIT ?", (limit * 2,)).fetchall()
        con.close()
    except sqlite3.Error:
        rows = []
    finally:
        shutil.rmtree(tmp, ignore_errors=True)
    out = [{"url": u, "title": t, "visits": int(v), "last": chromium_time(l)} for u, t, v, l in rows if web(u)]
    return out[:limit]


def bookmarks_from_html(path):
    try:
        text = open(path, encoding="utf-8", errors="replace").read()
    except OSError:
        return []
    import html
    out = []
    for m in re.finditer(r'<a\s[^>]*href="([^"]+)"[^>]*>(.*?)</a>', text, re.I | re.S):
        if web(m.group(1)):
            out.append({"url": m.group(1), "title": html.unescape(re.sub(r"<[^>]+>", "", m.group(2))).strip(),
                        "date": time.time()})
    return out


def merge_bookmarks(new):
    path = os.path.join(data_dir(), "bookmarks.json")
    have = read_json(path, []) or []
    seen = {b.get("url") for b in have}
    added = 0
    for b in new:
        if b["url"] not in seen:
            seen.add(b["url"])
            have.append(b)
            added += 1
    write_json(path, have)
    return added


def merge_history(new, keep=5000):
    path = os.path.join(data_dir(), "history.json")
    have = read_json(path, []) or []
    by = {h.get("url"): h for h in have}
    added = 0
    for h in new:
        cur = by.get(h["url"])
        if cur is None:
            by[h["url"]] = dict(h)
            added += 1
        else:
            cur["visits"] = max(cur.get("visits", 0), h["visits"])
            cur["last"] = max(cur.get("last", 0), h["last"])
            if not cur.get("title"):
                cur["title"] = h["title"]
    merged = sorted(by.values(), key=lambda h: h.get("last", 0), reverse=True)[:keep]
    write_json(path, merged)
    return added


def import_from(src, want_bookmarks, want_history, limit=3000):
    b = h = 0
    if want_bookmarks:
        items = firefox_bookmarks(src) if src.kind == "firefox" else chromium_bookmarks(src)
        b = merge_bookmarks(items)
    if want_history:
        items = firefox_history(src, limit) if src.kind == "firefox" else chromium_history(src, limit)
        h = merge_history(items)
    return b, h


def do_import(ask, args):
    step("Bringing your things over")
    say("Bookmarks and history can come over. Passwords and cookies don't: sign in again.")
    sources = find_sources()
    chosen = []
    if args.imp == "none":
        say("Skipping import.")
        return
    if args.imp == "all":
        chosen = list(range(len(sources)))
    elif args.imp:
        names = [n.lower() for n in args.imp.split(",")]
        chosen = [i for i, s in enumerate(sources) if any(n in s.label.lower() for n in names)]
    else:
        if not sources:
            say("No other browsers found. (You can import a bookmarks file later in Fasty: Bookmarks > Import.)")
            return
        chosen = ask.many("Import from which browser? (several are fine)", [s.label for s in sources])
    if not chosen:
        say("Nothing imported.")
        return
    what_b, what_h = True, True
    if not ask.auto and args.imp is None:
        what = ask.choose("What to bring over?", ["Bookmarks and history", "Bookmarks only", "History only"])
        what_b, what_h = what in (0, 1), what in (0, 2)
    tb = th = 0
    for i in chosen:
        b, h = import_from(sources[i], what_b, what_h)
        say(f"  {sources[i].label}: {b} bookmarks, {h} history entries")
        tb, th = tb + b, th + h
    say(f"Imported {tb} bookmarks and {th} history entries.")


# ---- the rest of the questions ------------------------------------------------------------------

def nvidia_present():
    return os.path.exists("/dev/nvidia-uvm")


def want_gui(args):
    """The window when there is a screen and GTK for Python, unless the text installer was asked for."""
    asked_for_text = args.cli or args.yes or args.uninstall or args.engine or args.imp or args.import_file \
        or args.default_browser or args.gpu_decode
    if asked_for_text and not args.gui:
        return False
    if not (os.environ.get("WAYLAND_DISPLAY") or os.environ.get("DISPLAY")):
        return False
    try:
        import gi
        gi.require_version("Gtk", "4.0")
        gi.require_version("Adw", "1")
        from gi.repository import Adw, Gtk  # noqa: F401
        return True
    except (ImportError, ValueError):
        dist = distro()
        if not args.gui and dist in GUI_PACKAGES and sys.stdin.isatty() and not args.yes:
            say("The installer window needs a few packages: " + " ".join(GUI_PACKAGES[dist]))
            if Ask(False).yes_no("Install them now? (it will ask for your password)", default=True):
                subprocess.run(install_command(dist, GUI_PACKAGES[dist]))
                os.execv(sys.executable, [sys.executable] + sys.argv)  # start again, now with the window
            return False
        if args.gui:
            pk = " ".join(GUI_PACKAGES.get(dist, []))
            sys.exit("The graphical installer needs Python's GTK 4 and libadwaita bindings." +
                     (f"\nInstall them with: {' '.join(install_command(dist, GUI_PACKAGES[dist]))}" if pk else ""))
        return False


def main():
    ap = argparse.ArgumentParser(description="Install Fasty Browser for your user.")
    ap.add_argument("--prefix", default=os.path.expanduser("~/.local"), help="where to install (default ~/.local)")
    ap.add_argument("--yes", action="store_true", help="don't ask; use the defaults and the options given")
    ap.add_argument("--default-browser", choices=["yes", "no"], help="make Fasty the default browser")
    ap.add_argument("--engine", help="search engine key: " + ", ".join(k for k, _ in ENGINES))
    ap.add_argument("--custom-engine", help="address with %%s for the query, with --engine custom")
    ap.add_argument("--import", dest="imp", help="none, all, or browser names such as 'firefox,chrome'")
    ap.add_argument("--import-file", help="a bookmarks .html file to import")
    ap.add_argument("--gpu-decode", choices=["yes", "no"], help="turn on GPU video decoding (NVIDIA)")
    ap.add_argument("--skip-checks", action="store_true", help="don't check for missing libraries")
    ap.add_argument("--no-launch", action="store_true", help="don't offer to start Fasty at the end")
    ap.add_argument("--gui", action="store_true", help="use the graphical installer (the default when there is a screen)")
    ap.add_argument("--cli", action="store_true", help="use the text installer")
    ap.add_argument("--uninstall", action="store_true", help="remove Fasty (your history and settings stay)")
    args = ap.parse_args()
    if want_gui(args):
        sys.argv = [sys.argv[0]]
        spec = importlib.util.spec_from_file_location("fasty_gui", os.path.join(HERE, "install_gui.py"))
        gui = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(gui)
        sys.exit(gui.run(args.prefix))
    if args.uninstall:
        uninstall(args.prefix)
        return
    ask = Ask(args.yes)

    say("Fasty Browser: a small, fast, quiet web browser.")
    binary = os.path.join(HERE, "fasty-browser-bin")
    if not os.path.exists(binary):
        sys.exit("fasty-browser-bin isn't next to this installer. Run it from the unpacked release folder.")

    ok = True if args.skip_checks else check_requirements(binary, ask, args.yes)
    if not ok and not ask.auto and not ask.yes_no("\nInstall Fasty anyway?", default=False):
        sys.exit("Stopped. Install what's missing, then run this again.")
    if not ok and ask.auto and not args.skip_checks:
        say("Some libraries are missing; Fasty may not start until they are installed.")

    link = install_files(args.prefix)

    step("Search engine")
    keys = [k for k, _ in ENGINES]
    if args.engine:
        idx = keys.index(args.engine) if args.engine in keys else 0
    else:
        idx = ask.choose("Which search engine should Fasty use?", [n for _, n in ENGINES], default=0)
    engine = keys[idx]
    kv = {"engine": engine}
    if engine == "custom":
        url = args.custom_engine or ask.text("Search address, with %s where the words go:", "https://duckduckgo.com/?q=%s")
        kv["custom_engine"] = url
    set_settings(**kv)
    say(f"Search engine: {dict(ENGINES)[engine]}.")

    step("Default browser")
    want = (args.default_browser == "yes") if args.default_browser else ask.yes_no(
        "Make Fasty your default web browser?", default=True)
    if want:
        set_default_browser()
    else:
        say("Leaving your default browser alone. (Settings > General has a button for it.)")

    if nvidia_present():
        step("GPU video decoding")
        say("You have an NVIDIA card. Fasty can decode video on it instead of the CPU (experimental).")
        say("It needs gst-plugin-va and libva-nvidia-driver, and adds two device files to the page sandbox.")
        gpu = (args.gpu_decode == "yes") if args.gpu_decode else ask.yes_no("Turn it on?", default=False)
        if gpu:
            set_settings(gpu_video_decode=True)
            say("On. Settings > General shows what is still missing, if anything.")

    if args.import_file:
        n = merge_bookmarks(bookmarks_from_html(args.import_file))
        say(f"Imported {n} bookmarks from {args.import_file}.")
    do_import(ask, args)

    step("Done")
    say(f"Start Fasty from your applications menu, or run:  {link}")
    if args.prefix == os.path.expanduser("~/.local") and os.path.expanduser("~/.local/bin") not in os.environ.get("PATH", "").split(":"):
        say("(Add ~/.local/bin to your PATH to start it by name.)")
    if not args.no_launch and not ask.auto and ask.yes_no("Start Fasty now?", default=True):
        subprocess.Popen([link], stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                         start_new_session=True)


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        sys.exit("\nStopped.")
