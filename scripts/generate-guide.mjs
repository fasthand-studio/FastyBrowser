import { chromium } from "@playwright/test";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const browser = await chromium.launch({ headless: true });
const page = await browser.newPage();
await page.emulateMedia({ media: "print" });
await page.goto(pathToFileURL(resolve("public/fasty-product-guide.html")).href, { waitUntil: "networkidle" });
await page.pdf({
  path: resolve("public/fasty-product-guide.pdf"),
  format: "A4",
  printBackground: true,
  preferCSSPageSize: true,
});
await browser.close();
