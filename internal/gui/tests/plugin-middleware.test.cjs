// Run with Node's test runner and Playwright on the module path; see README.md.
// Gateway middleware plugins on the Installed tab: a plugin that is only
// middleware says what it runs (its hooks, calls, µs each, failures with
// the last error in the tip) where a subscription plugin says what it
// signs in to, and never "Signs in to nothing magpie can use"; one that
// didn't load says why in red; one switched off says Off alone; a plugin
// that is both keeps its sign-in line and gains the middleware line.
// English and Chinese; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

const ALIAS = "/Users/x/mw/alias.middleware.js", BROKEN = "/Users/x/mw/broken.middleware.js", OFF = "/Users/x/mw/off.middleware.js", BOTH = "/Users/x/mw/both";
const state = { bun: true, bunVersion: "1.3.0", plugins: [
  { spec: ALIAS, providers: [], moved: [], middleware: { hooks: ["onRequest", "onEvent"], events: ["content_block_delta"], calls: 12345, avgMicros: 0.93, failures: 2, lastError: "onEvent: TypeError: x is undefined" } },
  { spec: BROKEN, providers: [], moved: [], middleware: { hooks: [], error: "exports none of onRequest, onEvent and onResponse", calls: 0, avgMicros: 0, failures: 0 } },
  { spec: OFF, off: true, providers: [], moved: [] },
  { spec: BOTH, providers: ["Acme"], moved: [], middleware: { hooks: ["onResponse"], calls: 0, avgMicros: 0, failures: 0 } },
] };

function server(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/plugins/market" || url.pathname === "/api/plugins" || url.pathname === "/api/plugins/listings") return json(url.pathname === "/api/plugins" ? state : url.pathname === "/api/plugins/listings" ? { listings: [] } : { listings: [], state });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = {
  en: { installed: "Installed", chip: "Middleware", line: "Gateway middleware: onRequest, onEvent · 12,345 calls, 0.9 µs each", failed: "2 failed", last: /Last: onEvent: TypeError: x is undefined/, events: /onEvent sees only content_block_delta events/, broken: /^Middleware didn't load: exports none/, off: "Off", nothing: "Signs in to nothing", both: "Signs in to Acme", bothLine: "Gateway middleware: onResponse" },
  zh: { installed: "已安装", chip: "中间件", line: "网关中间件：onRequest、onEvent · 调用 12,345 次，平均每次 0.9 µs", failed: "2 次失败", last: /最近一次：onEvent: TypeError: x is undefined/, events: /onEvent 只处理 content_block_delta 事件/, broken: /^中间件没有加载：exports none/, off: "关闭", nothing: "不能登录", both: "Acme", bothLine: "网关中间件：onResponse" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": gateway middleware plugins in Installed", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) {
      await t.test(lang, async () => {
        const w = L[lang];
        const page = await (await browser.newContext({ viewport: { width: 980, height: 820 } })).newPage();
        page.setDefaultTimeout(5000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang));
        await page.goto("http://magpie.test/?view=plugins");
        const view = page.locator("#view-plugins");
        await view.locator(".lib-tabs .opt", { hasText: w.installed }).click();
        const row = (name) => view.locator(".pm-row").filter({ has: page.locator(".name", { hasText: name }) });

        const alias = row("alias.middleware.js");
        await alias.waitFor();
        assert.equal((await alias.locator(".pm-chip").innerText()).trim(), w.chip);
        const line = alias.locator(".pm-mw");
        assert.equal((await line.innerText()).replace(/\s+/g, " ").trim(), w.line + " · " + w.failed);
        assert.match(await line.getAttribute("title"), w.events);
        assert.match(await line.locator(".pm-mw-fail").getAttribute("title"), w.last);
        assert.equal(await line.locator(".pm-mw-fail").evaluate((e) => getComputedStyle(e).color), await page.evaluate(() => { const d = document.createElement("span"); d.style.color = "var(--red)"; document.body.append(d); const c = getComputedStyle(d).color; d.remove(); return c; }));
        assert.ok(!(await alias.innerText()).includes(w.nothing), "a middleware plugin isn't said to sign in to nothing");

        const broken = row("broken.middleware.js");
        assert.match((await broken.locator(".pm-mw").innerText()).trim(), w.broken);
        assert.ok(await broken.locator(".pm-mw").evaluate((e) => e.classList.contains("bad")));
        assert.equal(await broken.locator(".pm-chip").count(), 0, "no Middleware chip on one that didn't load");

        const off = row("off.middleware.js");
        assert.equal((await off.locator(".who .sub").innerText()).trim(), w.off);
        assert.equal(await off.locator(".pm-mw").count(), 0);

        const both = row("both");
        assert.ok((await both.locator(".who .sub").first().innerText()).includes(w.both));
        assert.equal((await both.locator(".pm-mw").innerText()).trim(), w.bothLine);

        // nothing drawn with a coloured left border
        const borders = await view.locator(".pm-row *").evaluateAll((els) => els.filter((e) => { const s = getComputedStyle(e); return parseFloat(s.borderLeftWidth) > 0 && s.borderLeftStyle !== "none" && parseFloat(s.borderRightWidth) === 0; }).length);
        assert.equal(borders, 0);
        if (process.env.ARTIFACT_DIR) await view.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `plugin-middleware-${engine}-${lang}.png`) });
        assert.deepEqual(errors, []);
      });
    }
  });
}
