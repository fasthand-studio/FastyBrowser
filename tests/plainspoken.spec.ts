import { test, expect } from "@playwright/test";

const CONTROL_SELECTOR = "button, a.button, input, summary, .nav-links a, .download-secondary-links a, .mock-tab";

test.describe("PLAINspoken design-system conformance", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
    await expect(page.locator(".site-header .site-brand")).toBeVisible();
  });

  test("no shadows, gradients or blurs are rendered", async ({ page }) => {
    const offenders = await page.evaluate(() => {
      const found: string[] = [];
      document.querySelectorAll<HTMLElement>("*").forEach((element) => {
        const style = getComputedStyle(element);
        if (style.visibility === "hidden" || style.display === "none") return;
        const label = `${element.tagName.toLowerCase()}.${element.className}`;
        if (style.boxShadow !== "none") found.push(`shadow: ${label}`);
        if (/gradient\(/.test(style.backgroundImage)) found.push(`gradient: ${label}`);
        if (style.backdropFilter && style.backdropFilter !== "none") found.push(`blur: ${label}`);
      });
      return found;
    });
    expect(offenders).toEqual([]);
  });

  test("system font stack only, 17px body copy", async ({ page }) => {
    const body = page.locator("body");
    await expect(body).toHaveCSS("font-size", "17px");
    const stacks = await page.evaluate(() => {
      const families = new Set<string>();
      document.querySelectorAll<HTMLElement>("*").forEach((element) => {
        const style = getComputedStyle(element);
        if (style.display === "none") return;
        families.add(style.fontFamily.split(",")[0].trim().replace(/"/g, ""));
      });
      return [...families];
    });
    expect(stacks).toEqual(["system-ui"]);
  });

  test("interactive controls use exact half-height capsule geometry", async ({ page }) => {
    const mismatches = await page.evaluate((selector: string) => {
      const bad: string[] = [];
      document.querySelectorAll<HTMLElement>(selector).forEach((element) => {
        const style = getComputedStyle(element);
        const radius = parseFloat(style.borderTopLeftRadius);
        const height = element.getBoundingClientRect().height;
        if (!radius || !height) return;
        if (Math.abs(radius - height / 2) > 1) {
          bad.push(`${element.tagName.toLowerCase()}.${element.className} radius ${radius} vs ${height / 2}`);
        }
        if (!/superellipse/.test(style.cornerShape || "")) {
          bad.push(`${element.tagName.toLowerCase()}.${element.className} missing corner-shape`);
        }
      });
      return bad;
    }, CONTROL_SELECTOR);
    expect(mismatches).toEqual([]);
  });

  test("rounded surfaces use explicit pixel radii, never 50% or 9999px", async ({ page }) => {
    const offenders = await page.evaluate(() => {
      const found: string[] = [];
      document.querySelectorAll<HTMLElement>("*").forEach((element) => {
        const style = getComputedStyle(element);
        const radius = style.borderTopLeftRadius;
        if (!radius || radius === "0px") return;
        const label = `${element.tagName.toLowerCase()}.${element.className}`;
        if (radius.includes("%") || parseFloat(radius) >= 9999) found.push(label);
        if (!/superellipse/.test(style.cornerShape || "")) found.push(`${label} missing corner-shape`);
      });
      return found;
    });
    expect(offenders).toEqual([]);
  });

  test("borders only appear on text fields", async ({ page }) => {
    const bordered = await page.evaluate(() => {
      const found: string[] = [];
      document.querySelectorAll<HTMLElement>("*").forEach((element) => {
        const style = getComputedStyle(element);
        if (style.display === "none") return;
        const width = ["Top", "Right", "Bottom", "Left"].map((side) => parseFloat(style[`border${side}Width`] as unknown as string));
        if (width.some((value) => value > 0) && !element.closest(".mock-address, .mock-search")) {
          found.push(`${element.tagName.toLowerCase()}.${element.className}`);
        }
      });
      return found;
    });
    expect(bordered).toEqual([]);
  });

  test("no eyebrow chips sit above display headings", async ({ page }) => {
    await expect(page.locator(".section-eyebrow, .hero-kicker")).toHaveCount(0);
    const labels = await page.locator(".button").evaluateAll((nodes) => nodes.map((node) => node.textContent?.trim() ?? ""));
    labels.forEach((label) => expect(label.split(/\s+/).length).toBeLessThanOrEqual(5));
  });

  test("horizontal grids never resolve to three columns", async ({ page }) => {
    const triples = await page.evaluate(() => {
      const found: string[] = [];
      document.querySelectorAll<HTMLElement>("*").forEach((element) => {
        const style = getComputedStyle(element);
        if (style.display !== "grid" || style.gridTemplateColumns.split(" ").length !== 3) return;
        if (element.children.length === 3) found.push(`${element.tagName.toLowerCase()}.${element.className}`);
      });
      return found;
    });
    expect(triples).toEqual([]);
  });
});
