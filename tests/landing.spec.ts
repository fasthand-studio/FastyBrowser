import { expect, test } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  (page as typeof page & { runtimeErrors: string[] }).runtimeErrors = errors;
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Less noise. More web." })).toBeVisible();
});

test.afterEach(async ({ page }) => {
  const errors = (page as typeof page & { runtimeErrors?: string[] }).runtimeErrors ?? [];
  expect(errors).toEqual([]);
});

test("desktop landing page renders every major section without overflow", async ({ page }) => {
  for (const id of ["features", "preview", "technology", "download", "about", "docs", "faq"]) {
    await expect(page.locator(`#${id}`)).toBeAttached();
  }
  await expect(page.locator(".feature-card")).toHaveCount(8);
  await expect(page.locator(".demo-card")).toHaveCount(4);
  await expect(page.locator(".doc-card")).toHaveCount(4);

  const geometry = await page.evaluate(() => {
    const selectors = [".site-header", ".hero-section", ".feature-grid", ".product-preview", ".demo-grid", ".download-card", ".faq-layout", ".site-footer"];
    const boxes = Object.fromEntries(selectors.map((selector) => {
      const rect = document.querySelector(selector)!.getBoundingClientRect();
      return [selector, { x: rect.x, right: rect.right, width: rect.width }];
    }));
    return {
      viewport: innerWidth,
      scrollWidth: document.documentElement.scrollWidth,
      boxes,
      unnamedButtons: [...document.querySelectorAll("button")].filter((button) => !(button.getAttribute("aria-label") || button.textContent?.trim())).length,
      missingImageAlt: [...document.querySelectorAll("img")].filter((image) => !image.hasAttribute("alt")).length,
      emptyLinks: [...document.querySelectorAll("a")].filter((link) => !link.getAttribute("href")).length,
      missingSectionLabels: [...document.querySelectorAll("[aria-labelledby]")].filter((element) => !document.getElementById(element.getAttribute("aria-labelledby")!)).length,
    };
  });

  expect(geometry.scrollWidth).toBeLessThanOrEqual(geometry.viewport + 1);
  expect(geometry.unnamedButtons).toBe(0);
  expect(geometry.missingImageAlt).toBe(0);
  expect(geometry.emptyLinks).toBe(0);
  expect(geometry.missingSectionLabels).toBe(0);
  for (const box of Object.values(geometry.boxes)) {
    expect(box.x).toBeGreaterThanOrEqual(-1);
    expect(box.right).toBeLessThanOrEqual(geometry.viewport + 1);
    expect(box.width).toBeGreaterThan(0);
  }
  await page.screenshot({ path: "artifacts/landing-desktop.png", fullPage: true });
});

test("hero CTA reaches verified package downloads", async ({ page }) => {
  await page.getByRole("link", { name: "Download for Linux" }).click();
  await expect(page).toHaveURL(/#download$/);
  await expect(page.locator("#download")).toBeInViewport();
  await expect(page.getByText("Fasty Browser 1.1.1", { exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "Download AppImage" })).toHaveAttribute("href", "https://github.com/fasthand-studio/FastyBrowser/releases/download/v1.1.1/Fasty-1.1.1-x86_64.AppImage");

  await page.getByRole("tab", { name: "Debian / Ubuntu" }).click();
  await expect(page.locator(".package-file strong")).toHaveText("fasty-browser_1.1.1_amd64.deb");
  await expect(page.getByRole("link", { name: "Download Debian / Ubuntu" })).toHaveAttribute("href", "https://github.com/fasthand-studio/FastyBrowser/releases/download/v1.1.1/fasty-browser_1.1.1_amd64.deb");
  await expect(page.locator(".install-command pre")).toContainText("sudo apt install ./fasty-browser_1.1.1_amd64.deb");
  await page.screenshot({ path: "artifacts/landing-download-desktop.png" });
});

test("all four release assets and checksums are represented accurately", async ({ page }) => {
  const expected = [
    ["AppImage", "Fasty-1.1.1-x86_64.AppImage", "Fasty-1.1.1-x86_64.AppImage", "6239358c0643f817b88189da2ac6cbd0c0239c8d93ae3a68f89a50eb99d841dc"],
    ["Debian / Ubuntu", "fasty-browser_1.1.1_amd64.deb", "fasty-browser_1.1.1_amd64.deb", "08aa2050e519f56ebb1edbc973e14acf9cd3263573749d346ec1b4acb80e0a6b"],
    ["Fedora / RPM", "fasty-browser-1.1.1-1.x86_64.rpm", "fasty-browser-1.1.1-1.x86_64.rpm", "de52a87dd9be86afa2ab987dda17dc3ff9eef2719eecbd4ffbf8bd6e5d6afc5a"],
    ["Tarball", "fasty-browser-1.1.1-linux-x86_64.tar.gz", "fasty-browser-1.1.1-linux-x86_64.tar.gz", "f53ef8a507312fc76967c6cf4e2f871a70eaedfd594ea2560c69b5ab0c00842f"],
  ];

  for (const [label, filename, asset, checksum] of expected) {
    await page.getByRole("tab", { name: label, exact: true }).click();
    await expect(page.locator(".package-file strong")).toHaveText(filename);
    await expect(page.locator(".package-download")).toHaveAttribute("href", `https://github.com/fasthand-studio/FastyBrowser/releases/download/v1.1.1/${asset}`);
    await page.locator(".checksum-details summary").click();
    await expect(page.locator(".checksum-details code")).toHaveText(checksum);
    await page.locator(".checksum-details summary").click();
  }
  await expect(page.getByText(/No Windows, macOS, or ARM64 asset is published/)).toBeVisible();
});

test("interactive browser preview switches product scenes", async ({ page }) => {
  await page.getByRole("button", { name: /Tabs that rest/ }).click();
  await expect(page.getByRole("heading", { name: "Memory & speed" })).toBeVisible();
  await expect(page.getByRole("button", { name: /Tabs that rest/ })).toHaveAttribute("aria-pressed", "true");

  await page.getByRole("button", { name: /Agent control, with permission/ }).click();
  await expect(page.getByText("Only for sites you own or are allowed to test.")).toBeVisible();
  await expect(page.locator(".preview-scene-content")).toContainText("Opt-in");
  await expect(page.locator(".preview-disclaimer")).toContainText("No live browsing is performed");
  await page.screenshot({ path: "artifacts/landing-preview-desktop.png" });
});

test("feature demos respond to user input", async ({ page }) => {
  const sleep = page.locator(".sleep-demo");
  await sleep.getByRole("button", { name: "Let background tabs sleep" }).click();
  await expect(sleep.getByRole("button", { name: "Wake background tabs" })).toHaveAttribute("aria-pressed", "true");
  await expect(sleep.locator(".memory-meter")).toContainText("Lower");
  await expect(sleep.locator(".sleep-tabs .is-sleeping")).toHaveCount(3);

  const shield = page.locator(".shield-demo");
  await shield.getByRole("button", { name: /Ads/ }).click();
  await expect(shield.locator(".shield-summary")).toContainText("2 shield categories active");

  await page.getByRole("button", { name: "Pause demo video" }).click();
  await expect(page.getByRole("button", { name: "Play demo video" })).toBeVisible();

  await page.getByRole("button", { name: "Show demo code" }).click();
  await expect(page.locator(".authenticator-code strong")).toHaveText("482 915");
  await page.getByRole("button", { name: "Hide demo code" }).click();
  await expect(page.locator(".authenticator-code strong")).toHaveText("••• •••");
});

test("FAQ expands and documentation links are valid", async ({ page }) => {
  const binaryFaq = page.locator(".faq-list details").filter({ hasText: "Can I download a ready-to-run binary?" });
  await binaryFaq.locator("summary").click();
  await expect(binaryFaq).toHaveAttribute("open", "");
  await expect(binaryFaq).toContainText("Fasty 1.1.1 publishes AppImage, .deb, .rpm, and tarball assets");

  await expect(page.getByRole("link", { name: /Open install guide/ })).toHaveAttribute("href", "https://github.com/fasthand-studio/FastyBrowser/blob/main/docs/install.md");
  await expect(page.getByRole("link", { name: /Feature limitations/ })).toHaveAttribute("href", "https://github.com/fasthand-studio/FastyBrowser/blob/main/docs/features.md");
});

test("PDF guide, logo, license, and release manifest are downloadable", async ({ request }) => {
  const pdf = await request.get("/fasty-product-guide.pdf");
  expect(pdf.status()).toBe(200);
  expect(pdf.headers()["content-type"]).toContain("application/pdf");
  const pdfBody = await pdf.body();
  expect(pdfBody.subarray(0, 5).toString()).toBe("%PDF-");
  expect(pdfBody.length).toBeGreaterThan(50_000);

  const logo = await request.get("/fasty-logo.svg");
  expect(logo.status()).toBe(200);
  expect(await logo.text()).toContain("#5301FE");

  const license = await request.get("/LICENSE.fasty.txt");
  expect(license.status()).toBe(200);
  expect(await license.text()).toContain("MIT License");

  const sums = await request.get("https://github.com/fasthand-studio/FastyBrowser/releases/download/v1.1.1/SHA256SUMS");
  expect(sums.status()).toBe(200);
  expect(await sums.text()).toContain("Fasty-1.1.1-x86_64.AppImage");
});

test("mobile layout, navigation, and previews stay inside the viewport", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.getByRole("button", { name: "Open navigation" })).toBeVisible();
  await page.getByRole("button", { name: "Open navigation" }).click();
  const mobileNav = page.getByRole("navigation", { name: "Mobile navigation" });
  await expect(mobileNav).toBeVisible();
  await mobileNav.getByRole("link", { name: "Download", exact: true }).click();
  await expect(mobileNav).toHaveCount(0);
  await expect(page).toHaveURL(/#download$/);
  await expect(page.locator("#download")).toBeInViewport();

  const geometry = await page.evaluate(() => {
    const selectors = [".site-header", ".hero-copy", ".hero-browser", ".feature-grid", ".product-preview", ".demo-grid", ".download-card", ".site-footer"];
    return {
      viewport: innerWidth,
      scrollWidth: document.documentElement.scrollWidth,
      boxes: Object.fromEntries(selectors.map((selector) => {
        const rect = document.querySelector(selector)!.getBoundingClientRect();
        return [selector, { x: rect.x, right: rect.right, width: rect.width }];
      })),
      featureColumns: getComputedStyle(document.querySelector(".feature-grid")!).gridTemplateColumns,
    };
  });
  expect(geometry.scrollWidth).toBeLessThanOrEqual(geometry.viewport + 1);
  for (const box of Object.values(geometry.boxes)) {
    expect(box.x).toBeGreaterThanOrEqual(-1);
    expect(box.right).toBeLessThanOrEqual(geometry.viewport + 1);
    expect(box.width).toBeGreaterThan(0);
  }
  expect(geometry.featureColumns.split(" ")).toHaveLength(1);

  await page.goto("/");
  await page.screenshot({ path: "artifacts/landing-mobile.png", fullPage: true });
  await page.screenshot({ path: "artifacts/landing-hero-mobile.png" });
});
