// Run with Node's test runner and Playwright on the module path; see README.md.
// #116 (marsxxl, v0.1.768): a removed Gemini CLI the user had asked not to
// be reminded of sat under Add a provider's "Removed from magpie — still
// signed in" with no way to put it away: "Don't remind me" was only on the
// Providers page's reminder line, which that choice had already hidden. Its
// row's menu now has "Don't show here" too (POST /api/provider/tuck): the
// sheet stays open, the row goes and stays gone after a reload (the backend
// keeps it), and a "Show 1 hidden" link in the section's heading lists it
// again, where "Show here again" (untuck) brings it back for good; a search
// naming it finds it as well. Clicks never move the page, no native select.
// The reminder line keeps its "Don't remind me". English and Chinese; the
// API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

const codex = { id: "codex", name: "Codex", icon: "codex", models: [], agents: [], key: {}, account: { agent: "codex", agentName: "Codex", agentIcon: "codex", user: "a@b", logins: [{ user: "a@b", active: true, on: true }] } };

function server(lang, asked, st) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    const state = () => ({
      providers: [codex], presets: [], plugins: [], gateway: { running: true, window: true },
      excluded: [{ agent: "gemini", provider: "gemini", why: "You removed it from magpie.", quiet: st.quiet, tucked: st.tucked, agentName: "Gemini CLI", agentIcon: "gemini" }],
    });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json(state());
    if (url.pathname.startsWith("/api/provider/")) {
      const a = url.pathname.slice("/api/provider/".length);
      asked.push([a, route.request().postDataJSON()]);
      if (a === "tuck") st.tucked = st.quiet = true;
      if (a === "untuck") st.tucked = false;
      if (a === "quiet") st.quiet = true;
      return json(state());
    }
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = {
  en: { hide: "Don't show here", again: "Show here again", more: "Show 1 hidden", less: "Hide the hidden", back: "Add it back", out: "Sign out…", quiet: "Don't remind me" },
  zh: { hide: "不在这里显示", again: "重新在这里显示", more: "显示 1 个已隐藏", less: "收起已隐藏", back: "加回来", out: "退出登录…", quiet: "不再提示" },
};

const scrolls = (page) => page.evaluate(() => [scrollY, ...[...document.querySelectorAll("*")].filter((e) => e.scrollTop).map((e) => e.scrollTop)].join(","));

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a removed row can be hidden from the add sheet, and found again", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) {
      await t.test(lang + ": Don't show here, Show hidden, Show here again", async () => {
        const w = L[lang];
        const page = await (await browser.newContext({ viewport: { width: 900, height: 520 } })).newPage();
        page.setDefaultTimeout(5000);
        const errors = [], asked = [], st = { quiet: true, tucked: false };
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, asked, st));
        await page.goto("http://magpie.test/?view=providers");
        await page.locator("#addProvider").click();
        const sheet = page.locator("#addSheet");
        const row = sheet.locator(`.tile[data-pick="removed:gemini"]`);
        await row.waitFor();
        await sheet.evaluate((el) => Promise.all(el.getAnimations().map((a) => a.finished)));
        assert.equal(await sheet.locator(".kind-more").count(), 0, "nothing hidden yet");

        // the row's menu offers to hide it, beside Add it back and Sign out
        await row.click();
        const menu = page.locator(".proto-menu");
        await menu.waitFor();
        assert.equal(await page.locator("select").count(), 0);
        assert.deepEqual((await menu.locator(".pm-name").allInnerTexts()).map((s) => s.trim()), [w.back, w.out, w.hide]);
        const y = await scrolls(page);
        await menu.locator(".pm-item", { hasText: w.hide }).click();
        await page.waitForFunction(() => !document.querySelector('#addSheet .tile[data-pick="removed:gemini"]'));
        assert.deepEqual(asked, [["tuck", { id: "gemini" }]]);
        assert.equal(await sheet.isVisible(), true, "the sheet stays open");
        assert.equal(await scrolls(page), y, "hiding it moved the page");

        // a link in the section's heading says it is there
        const more = sheet.locator(".kind-more");
        assert.equal((await more.innerText()).trim(), w.more);
        if (process.env.ARTIFACT_DIR) await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `removed-hide-${engine}-${lang}.png`) });

        // after a reload (a restart) it is still hidden
        await page.reload();
        await page.locator("#addProvider").click();
        await sheet.locator(".kind-more").waitFor();
        assert.equal(await row.count(), 0, "hidden again after a reload");

        // Show hidden lists it, the page where it was
        await sheet.evaluate((el) => Promise.all(el.getAnimations().map((a) => a.finished)));
        const y2 = await scrolls(page);
        await more.click();
        await row.waitFor();
        assert.equal(await scrolls(page), y2, "Show hidden moved the page");
        assert.equal((await more.innerText()).trim(), w.less);
        assert.match(await row.getAttribute("class"), /\btucked\b/);
        await row.click();
        await menu.waitFor();
        assert.deepEqual((await menu.locator(".pm-name").allInnerTexts()).map((s) => s.trim()), [w.back, w.out, w.again]);
        await menu.locator(".pm-item", { hasText: w.again }).click();
        await page.waitForFunction(() => !document.querySelector("#addSheet .kind-more"));
        assert.deepEqual(asked.at(-1), ["untuck", { id: "gemini" }]);
        await row.waitFor();
        assert.doesNotMatch(await row.getAttribute("class"), /\btucked\b/);
        assert.deepEqual(errors, []);
      });

      await t.test(lang + ": a search naming a hidden one finds it", async () => {
        const page = await (await browser.newContext({ viewport: { width: 900, height: 700 } })).newPage();
        page.setDefaultTimeout(5000);
        await page.route("**/*", server(lang, [], { quiet: true, tucked: true }));
        await page.goto("http://magpie.test/?view=providers");
        await page.locator("#addProvider").click();
        await page.locator("#addSheet .kind-more").waitFor();
        await page.locator("#addSheet .find").fill("gemini");
        await page.locator(`#addSheet .tile[data-pick="removed:gemini"]`).waitFor();
      });

      await t.test(lang + ": the reminder line keeps Don't remind me", async () => {
        const w = L[lang];
        const page = await (await browser.newContext({ viewport: { width: 900, height: 700 } })).newPage();
        page.setDefaultTimeout(5000);
        const asked = [];
        await page.route("**/*", server(lang, asked, { quiet: false, tucked: false }));
        await page.goto("http://magpie.test/?view=providers");
        const line = page.locator("#excluded .excluded");
        await line.waitFor();
        await line.locator("button.link", { hasText: new RegExp("^" + w.quiet + "$") }).click();
        await page.waitForFunction(() => !document.querySelector("#excluded .excluded"));
        assert.deepEqual(asked, [["quiet", { id: "gemini" }]]);
      });
    }
  });
}
