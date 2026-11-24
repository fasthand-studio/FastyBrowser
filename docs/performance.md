# Memory and speed

On one test machine (Arch, NVIDIA RTX 3060, seven pinned tabs, one page
loaded) the browser used about 470 MB in total: the window about 165 MB, a
typical page about 260 MB and the network process about 50 MB. A heavy page
such as YouTube takes 600 MB or more. What keeps it down:

- Tabs you haven't used for 5 minutes (Settings → General, down to 1 minute) go to
  sleep: their page process is ended, which gives the memory back, and the page
  reloads when you return. Tabs playing sound stay awake. They also sleep sooner
  when the system is short of memory. With ten tabs open (Wikipedia, Hacker News,
  Reddit, YouTube, GitHub, Facebook, Instagram, Cloudflare, Chess.com and a shop)
  the total was 3.8 GB with sleeping off and 0.78 GB with it on, one tab awake.
  Under 300 MB isn't possible: the window itself is about 180 to 260 MB, the
  network process 60 MB and a single page 100 to 600 MB.
- The C heap is trimmed after the blocker's rules are built.
- A page process starts dropping caches at about 150 MB and everything it can at
  about 300 MB (WebKit's own defaults wait until about 500 and 900 MB). On seven
  logged-out sites this took the total from 2.5 to 3.1 GB down to about 2.1 GB,
  for a few percent more idle CPU.
- The back/forward page cache is off, so a page you leave is let go instead of
  being kept alive for Back. Back reloads from the HTTP cache.
- The blocker's compiled rules are cached by content, so startup doesn't
  rebuild them.

Keep **Hardware acceleration** on: turning it off cost about 28% of the
Speedometer 3.1 score in testing. Fasty also avoids work that slows pages: for
example `assets/js/compat.js` stops GitHub's tab bars from running a
scroll-driven animation that halved the frame rate while scrolling.

## NVIDIA graphics

On NVIDIA's proprietary driver Fasty draws the window with OpenGL instead of
Vulkan, and turns off WebKit's DMA-BUF path to the compositor
(`WEBKIT_DISABLE_DMABUF_RENDERER=1`). In a scripted scroll of a Wikipedia page the
page process used 3 s of CPU in 10 s instead of 6 s. Set either variable
(`GSK_RENDERER`, `WEBKIT_DISABLE_DMABUF_RENDERER`) yourself to override.
