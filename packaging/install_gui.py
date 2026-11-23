#!/usr/bin/env python3
"""Fasty Browser graphical installer (GTK 4 + libadwaita).

Steps: welcome, what Fasty needs, search engine, default browser, bringing your
things over, then install. All the real work is in install.py; this is the
window around it.
"""
import importlib.util
import os
import subprocess
import sys
import threading

import gi

gi.require_version("Gtk", "4.0")
gi.require_version("Adw", "1")
from gi.repository import Adw, GLib, Gtk  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("fasty_install", os.path.join(HERE, "install.py"))
core = importlib.util.module_from_spec(spec)
spec.loader.exec_module(core)

# Plain characters rather than icon names, so it looks right on any icon theme.
GLYPH = {"emblem-ok-symbolic": "✓", "dialog-warning-symbolic": "⚠", "dialog-information-symbolic": "ℹ",
         "computer-symbolic": "🖥"}

WELCOME, NEEDS, ENGINE, DEFAULT, IMPORT, WORK, DONE = range(7)


def read_version():
    try:
        with open(os.path.join(HERE, "VERSION")) as f:
            return f.read().strip()
    except OSError:
        return ""


class Installer(Adw.ApplicationWindow):
    def __init__(self, app, prefix):
        super().__init__(application=app, title="Install Fasty", default_width=560, default_height=680)
        self.prefix = prefix
        self.binary = os.path.join(HERE, "fasty-browser-bin")
        self.needs = None
        self.engine = "duckduckgo"
        self.sources = []
        self.link = ""
        self.errors = []

        self.stack = Gtk.Stack(transition_type=Gtk.StackTransitionType.SLIDE_LEFT_RIGHT, vexpand=True)
        self.stack.add_named(self.page_welcome(), "0")
        self.stack.add_named(self.page_needs(), "1")
        self.stack.add_named(self.page_engine(), "2")
        self.stack.add_named(self.page_default(), "3")
        self.stack.add_named(self.page_import(), "4")
        self.stack.add_named(self.page_work(), "5")
        self.stack.add_named(self.page_done(), "6")
        self.page = WELCOME

        self.back = Gtk.Button(label="Back")
        self.back.connect("clicked", lambda *_: self.go(self.page - 1))
        self.next = Gtk.Button(label="Continue", css_classes=["suggested-action", "pill"])
        self.next.connect("clicked", self.on_next)
        bar = Gtk.CenterBox(margin_start=18, margin_end=18, margin_top=10, margin_bottom=14)
        bar.set_start_widget(self.back)
        bar.set_end_widget(self.next)
        self.bar = bar

        head = Adw.HeaderBar(show_title=False)
        view = Adw.ToolbarView(content=self.stack)
        view.add_top_bar(head)
        view.add_bottom_bar(bar)
        self.set_content(view)
        self.go(WELCOME)

    # ---- pages ---------------------------------------------------------------------------------

    def prefs(self, title="", desc=""):
        page = Adw.PreferencesPage()
        g = Adw.PreferencesGroup(title=title, description=desc)
        page.add(g)
        return page, g

    def page_welcome(self):
        sp = Adw.StatusPage(title="Fasty Browser", vexpand=True)
        icon = os.path.join(HERE, "fasty-browser.svg")
        if os.path.exists(icon):
            sp.set_paintable(Gtk.Image.new_from_file(icon).get_paintable())
        v = read_version()
        sp.set_description("A small, fast, quiet web browser." + (f"\nVersion {v}" if v else "") +
                           "\n\nThis installs Fasty for your user only. It won't ask for a password "
                           "unless something it needs is missing.")
        return sp

    def page_needs(self):
        page, self.needs_group = self.prefs("What Fasty needs", "Checking this computer…")
        self.needs_rows = []
        self.needs_actions = Adw.PreferencesGroup()
        page.add(self.needs_actions)
        self.install_btn = Gtk.Button(label="Install what's missing", css_classes=["suggested-action", "pill"],
                                      halign=Gtk.Align.CENTER, visible=False)
        self.install_btn.connect("clicked", self.install_packages)
        self.needs_actions.add(self.install_btn)
        self.pkg_spin = Gtk.Spinner(halign=Gtk.Align.CENTER)
        self.needs_actions.add(self.pkg_spin)
        self.pkg_log = Gtk.Label(wrap=True, xalign=0, css_classes=["dim-label", "caption"], selectable=True)
        self.needs_actions.add(self.pkg_log)
        self.force = Adw.SwitchRow(title="Install anyway", subtitle="Fasty may not start until these are installed.",
                                   visible=False)
        self.force.connect("notify::active", lambda *_: self.update_buttons())
        self.needs_actions.add(self.force)
        return page

    def page_engine(self):
        page, g = self.prefs("Search engine", "What the address bar uses when you type a search. You can change it later.")
        first = None
        self.engine_checks = {}
        self.custom = Adw.EntryRow(title="Search address, with %s where the words go", visible=False)
        self.custom.set_text("https://duckduckgo.com/?q=%s")
        for key, name in core.ENGINES:
            row = Adw.ActionRow(title=name, activatable=True)
            chk = Gtk.CheckButton(valign=Gtk.Align.CENTER)
            if first is None:
                first = chk
            else:
                chk.set_group(first)
            chk.connect("toggled", self.on_engine, key)
            row.add_suffix(chk)
            row.set_activatable_widget(chk)
            g.add(row)
            self.engine_checks[key] = chk
        g.add(self.custom)
        self.engine_checks["duckduckgo"].set_active(True)
        return page

    def on_engine(self, chk, key):
        if chk.get_active():
            self.engine = key
            self.custom.set_visible(key == "custom")

    def page_default(self):
        page, g = self.prefs("Default browser", "Links from other apps open in Fasty.")
        self.default_row = Adw.SwitchRow(title="Make Fasty my default browser", active=True)
        g.add(self.default_row)
        self.gpu_row = None
        if core.nvidia_present():
            g2 = Adw.PreferencesGroup(title="GPU video decoding",
                                      description="You have an NVIDIA card. Fasty can decode video on it instead of the "
                                                  "CPU (experimental). It needs gst-plugin-va and libva-nvidia-driver.")
            self.gpu_row = Adw.SwitchRow(title="Decode video on the GPU", active=False)
            g2.add(self.gpu_row)
            page.add(g2)
        return page

    def page_import(self):
        page, self.import_group = self.prefs(
            "Bring your things over",
            "Bookmarks and history can come over. Passwords and cookies don't: sign in again.")
        self.import_rows = []
        self.import_what = Adw.ComboRow(title="What to bring", model=Gtk.StringList.new(
            ["Bookmarks and history", "Bookmarks only", "History only"]))
        self.import_options = Adw.PreferencesGroup()
        self.import_options.add(self.import_what)
        self.html_row = Adw.ActionRow(title="A bookmarks file (.html)", subtitle="None chosen")
        pick = Gtk.Button(label="Choose…", valign=Gtk.Align.CENTER)
        pick.connect("clicked", self.pick_html)
        self.html_row.add_suffix(pick)
        self.html_path = ""
        self.import_options.add(self.html_row)
        page.add(self.import_options)
        return page

    def pick_html(self, *_):
        d = Gtk.FileDialog(title="Choose a bookmarks file")
        f = Gtk.FileFilter(name="Bookmarks HTML")
        f.add_pattern("*.html")
        f.add_pattern("*.htm")
        from gi.repository import Gio
        fl = Gio.ListStore.new(Gtk.FileFilter)
        fl.append(f)
        d.set_filters(fl)

        def done(dlg, res):
            try:
                gf = dlg.open_finish(res)
            except GLib.Error:
                return
            self.html_path = gf.get_path()
            self.html_row.set_subtitle(self.html_path)
        d.open(self, None, done)

    def page_work(self):
        box = Adw.StatusPage(title="Installing…", vexpand=True)
        self.work_page = box
        sp = Gtk.Spinner(spinning=True, width_request=48, height_request=48, halign=Gtk.Align.CENTER)
        box.set_child(sp)
        return box

    def page_done(self):
        self.done_page = Adw.StatusPage(title="Fasty is installed", vexpand=True)
        self.app_icon = None
        icon = os.path.join(HERE, "fasty-browser.svg")
        if os.path.exists(icon):
            self.app_icon = Gtk.Image.new_from_file(icon).get_paintable()
            self.done_page.set_paintable(self.app_icon)
        box = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=12, halign=Gtk.Align.CENTER)
        self.done_log = Gtk.Label(wrap=True, justify=Gtk.Justification.CENTER)
        self.start_btn = Gtk.Button(label="Start Fasty", css_classes=["suggested-action", "pill"], halign=Gtk.Align.CENTER)
        self.start_btn.connect("clicked", self.on_start)
        box.append(self.done_log)
        box.append(self.start_btn)
        self.done_page.set_child(box)
        return self.done_page

    # ---- moving between pages ---------------------------------------------------------------------

    def go(self, n):
        self.page = max(WELCOME, n)
        self.stack.set_visible_child_name(str(self.page))
        self.back.set_visible(WELCOME < self.page < WORK)
        self.next.set_visible(self.page < WORK)
        self.next.set_label({WELCOME: "Get started", IMPORT: "Install"}.get(self.page, "Continue"))
        if self.page == NEEDS:
            self.run_check()
        if self.page == IMPORT:
            self.fill_sources()
        self.update_buttons()

    def update_buttons(self):
        ok = True
        if self.page == NEEDS:
            ok = self.needs is not None and (self.needs.ok or self.force.get_active()) and not self.busy_pkgs
        self.next.set_sensitive(ok)

    busy_pkgs = False

    def on_next(self, *_):
        if self.page == IMPORT:
            self.start_install()
        else:
            self.go(self.page + 1)

    # ---- checking and installing packages -------------------------------------------------------------

    def run_check(self):
        self.needs = None
        self.needs_group.set_description("Checking this computer…")
        self.update_buttons()

        def work():
            n = core.Needs(self.binary)
            GLib.idle_add(self.show_needs, n)
        threading.Thread(target=work, daemon=True).start()

    def show_needs(self, n):
        self.needs = n
        for r in self.needs_rows:
            self.needs_group.remove(r)
        self.needs_rows = []

        def add(title, subtitle, icon):
            row = Adw.ActionRow(title=title, subtitle=subtitle)
            row.add_prefix(Gtk.Label(label=GLYPH.get(icon, "•")))
            self.needs_group.add(row)
            self.needs_rows.append(row)
        add("Your system", n.name, "dialog-information-symbolic")
        if n.too_old:
            add("This system is too old", n.problems[0][0], "dialog-warning-symbolic")
        for what, pkg in n.problems:
            if what is n.problems[0][0] and n.too_old:
                continue
            add(what, pkg or "install it with your package manager", "dialog-warning-symbolic")
        if n.media:
            add("Video and audio playback", ", ".join(n.media), "dialog-warning-symbolic")
        if n.keyring:
            add("Keyring tool", n.keyring + " keeps passkeys and Authenticator keys in the system keyring",
                "dialog-information-symbolic")
        if n.note and n.dist != "fedora":
            add("Note", n.note, "dialog-information-symbolic")
        if n.dist == "fedora" and n.media:
            add("Video codecs", "Fedora leaves out H.264, so this also turns on RPM Fusion (free) for it",
                "dialog-information-symbolic")
        if n.ok and not n.media and not n.keyring:
            add("Everything Fasty needs is here", "", "emblem-ok-symbolic")
        self.needs_group.set_description("")
        pk = n.packages
        self.install_btn.set_visible(bool(pk) and n.dist in core.INSTALL_ARGV)
        if pk and n.dist in core.INSTALL_ARGV and n.ok:
            self.install_btn.set_label("Install the extras")
        self.force.set_visible(not n.ok)
        if n.ok and not n.media:
            self.pkg_log.set_text("")
        self.update_buttons()

    def install_packages(self, *_):
        n = self.needs
        argv = n.argv(["pkexec"])
        self.busy_pkgs = True
        self.install_btn.set_sensitive(False)
        self.pkg_spin.start()
        self.pkg_log.set_text("Installing " + ", ".join(n.packages) + "…\nYour system will ask for your password.")
        self.update_buttons()

        def work():
            try:
                p = subprocess.run(argv, capture_output=True, text=True)
                tail = (p.stdout + p.stderr).strip().splitlines()[-3:]
                msg = "" if p.returncode == 0 else "That didn't finish (" + (" ".join(tail) or f"exit {p.returncode}") + ")."
            except OSError as e:
                msg = f"Couldn't start the package manager: {e}"
            GLib.idle_add(self.packages_done, msg)
        threading.Thread(target=work, daemon=True).start()

    def packages_done(self, msg):
        self.busy_pkgs = False
        self.pkg_spin.stop()
        self.install_btn.set_sensitive(True)
        self.pkg_log.set_text(msg)
        self.run_check()

    # ---- importing ----------------------------------------------------------------------------------------

    def fill_sources(self):
        if self.import_rows:
            return
        self.sources = core.find_sources()
        if not self.sources:
            self.import_group.set_description(
                "Passwords and cookies don't come over: sign in again.\nNo other browsers were found.")
        for s in self.sources:
            row = Adw.SwitchRow(title=s.label, active=False)
            self.import_group.add(row)
            self.import_rows.append(row)
        self.import_options.set_visible(True)

    # ---- the install itself --------------------------------------------------------------------------------

    def start_install(self):
        self.go(WORK)
        engine = self.engine
        custom = self.custom.get_text().strip()
        default = self.default_row.get_active()
        gpu = bool(self.gpu_row and self.gpu_row.get_active())
        picks = [s for s, r in zip(self.sources, self.import_rows) if r.get_active()]
        what = self.import_what.get_selected()
        html = self.html_path

        def work():
            log, errors = [], []
            try:
                self.link = core.install_files(self.prefix)
                log.append("Installed Fasty.")
            except Exception as e:  # noqa: BLE001
                errors.append(f"Couldn't install the files: {e}")
                GLib.idle_add(self.finish, log, errors)
                return
            kv = {"engine": engine}
            if engine == "custom" and custom:
                kv["custom_engine"] = custom
            core.set_settings(**kv)
            log.append("Search engine: " + dict(core.ENGINES)[engine] + ".")
            if default:
                if core.set_default_browser():
                    log.append("Fasty is your default browser.")
                else:
                    errors.append("Couldn't make Fasty the default browser. Settings > General has a button for it.")
            if gpu:
                core.set_settings(gpu_video_decode=True)
                log.append("GPU video decoding is on.")
            tb = th = 0
            for src in picks:
                try:
                    b, h = core.import_from(src, what in (0, 1), what in (0, 2))
                    tb, th = tb + b, th + h
                except Exception as e:  # noqa: BLE001
                    errors.append(f"Couldn't read {src.label}: {e}")
            if html:
                tb += core.merge_bookmarks(core.bookmarks_from_html(html))
            if picks or html:
                log.append(f"Brought over {tb} bookmarks and {th} history entries.")
            GLib.idle_add(self.finish, log, errors)
        threading.Thread(target=work, daemon=True).start()

    def finish(self, log, errors):
        self.go(DONE)
        failed = bool(errors) and not self.link
        self.done_page.set_title("Something went wrong" if failed else "Fasty is installed")
        self.done_page.set_paintable(None if failed else self.app_icon)
        self.done_log.set_text("\n".join(log + errors))
        self.start_btn.set_visible(not failed)
        self.back.set_visible(False)
        self.next.set_visible(False)
        self.start_btn.grab_focus()

    def on_start(self, *_):
        subprocess.Popen([self.link], stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                         stderr=subprocess.DEVNULL, start_new_session=True)
        self.close()


class App(Adw.Application):
    def __init__(self, prefix):
        super().__init__(application_id="app.fastybrowser.Installer")
        self.prefix = prefix

    def do_activate(self):
        w = self.props.active_window or Installer(self, self.prefix)
        w.present()


def run(prefix):
    return App(prefix).run([sys.argv[0]])


if __name__ == "__main__":
    sys.exit(run(os.path.expanduser("~/.local")))
