import importlib.util
import json
import os
import sqlite3
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("inst", os.path.join(os.path.dirname(__file__), "install.py"))
inst = importlib.util.module_from_spec(spec)
spec.loader.exec_module(inst)


class Fake(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = self.tmp.name
        os.environ["XDG_DATA_HOME"] = os.path.join(self.root, "data")
        self.ff = os.path.join(self.root, "ff")
        self.ch = os.path.join(self.root, "ch")
        os.makedirs(self.ff)
        os.makedirs(self.ch)

    def tearDown(self):
        self.tmp.cleanup()

    def firefox(self):
        con = sqlite3.connect(os.path.join(self.ff, "places.sqlite"))
        con.executescript("""
        create table moz_places(id integer primary key,url text,title text,visit_count int,last_visit_date int);
        create table moz_bookmarks(id integer primary key,type int,fk int,title text,dateAdded int);
        insert into moz_places values(1,'https://a.example/','A',5,1700000000000000),(2,'place:x','F',0,0),(3,'https://b.example/','',0,0);
        insert into moz_bookmarks values(1,1,1,'A mark',1690000000000000),(2,1,3,'',0),(3,2,NULL,'folder',0);
        """)
        con.commit()
        con.close()
        return inst.Source("Firefox", "firefox", self.ff, "Firefox")

    def chromium(self):
        json.dump({"roots": {"bookmark_bar": {"type": "folder", "children": [
            {"type": "url", "name": "C", "url": "https://c.example/", "date_added": "13340000000000000"},
            {"type": "folder", "children": [{"type": "url", "name": "", "url": "https://d.example/", "date_added": "0"}]}]}}},
            open(os.path.join(self.ch, "Bookmarks"), "w"))
        con = sqlite3.connect(os.path.join(self.ch, "History"))
        con.executescript("""create table urls(id integer primary key,url text,title text,visit_count int,last_visit_time int);
        insert into urls values(1,'https://c.example/h','H',3,13340000000000000),(2,'chrome://settings','s',1,13340000000000100);""")
        con.commit()
        con.close()
        return inst.Source("Chromium", "chromium", self.ch, "Chromium")

    def test_firefox(self):
        b, h = inst.import_from(self.firefox(), True, True)
        self.assertEqual((b, h), (2, 1))  # the folder and place: entries are left out
        marks = json.load(open(os.path.join(inst.data_dir(), "bookmarks.json")))
        self.assertEqual({m["url"]: m["title"] for m in marks},
                         {"https://a.example/": "A mark", "https://b.example/": "https://b.example/"})

    def test_chromium(self):
        b, h = inst.import_from(self.chromium(), True, True)
        self.assertEqual((b, h), (2, 1))  # chrome:// is left out; nested bookmarks are found
        hist = json.load(open(os.path.join(inst.data_dir(), "history.json")))
        self.assertEqual(hist[0]["url"], "https://c.example/h")
        self.assertAlmostEqual(hist[0]["last"], 13340000000 - 11644473600, delta=1)  # converted to Unix time

    def test_twice_adds_nothing(self):
        src = self.firefox()
        inst.import_from(src, True, True)
        self.assertEqual(inst.import_from(src, True, True), (0, 0))

    def test_private_and_merged_settings(self):
        inst.set_settings(engine="kagi")
        inst.set_settings(gpu_video_decode=True)
        path = os.path.join(inst.data_dir(), "settings.json")
        self.assertEqual(json.load(open(path)), {"engine": "kagi", "gpu_video_decode": True})
        self.assertEqual(oct(os.stat(path).st_mode & 0o777), "0o600")
        self.assertEqual(oct(os.stat(inst.data_dir()).st_mode & 0o777), "0o700")

    def test_html_bookmarks(self):
        p = os.path.join(self.root, "b.html")
        open(p, "w").write('<DL><A HREF="https://e.example/" ADD_DATE="1">E &amp; F</A><A HREF="ftp://x">no</A></DL>')
        self.assertEqual([(b["url"], b["title"]) for b in inst.bookmarks_from_html(p)], [("https://e.example/", "E & F")])


if __name__ == "__main__":
    unittest.main()
