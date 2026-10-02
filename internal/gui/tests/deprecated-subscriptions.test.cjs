// Run with Node's test runner and Playwright on the module path; see README.md.
// Built-in subscriptions that a community plugin can run are deprecated: their
// rows and Add tiles carry a Deprecated badge whose tooltip says why (a
// subscription can break its vendor's terms, so it is decoupled from magpie to
// keep magpie itself from being banned), and a notice over the list names the
// signed-in ones with the same reason. One already on its plugin, or a
// subscription with no plugin, carries no badge; Not now hides the notice
// until another deprecated subscription signs in. In English and Chinese,
// Chromium and WebKit; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const pkg = (id) => `@magpie-community/opencode-${id}-auth`;
const sub = (id, name, state) => ({
  id, name, icon: id, chat: "", responses: "", anthropic: "", catalog: "", models: [{ id: "m", name: "M", on: true }],
  agents: [], fallback: [], headers: {}, keyList: [], key: {},
  account: { agent: id, agentName: name, user: "ada", logins: [{ user: "ada", active: true, on: true, own: true }] },
  move: { package: pkg(id), state },
});

function serve(lang) {
  const providers = [
    sub("cursor", "Cursor", ""), sub("kiro", "Kiro", ""), sub("grok", "Grok", "plugin"),
    { id: "deepseek", name: "DeepSeek", icon: "deepseek", chat: "https://api.deepseek.com", responses: "", anthropic: "", catalog: "", models: [{ id: "m", name: "M", on: true }], agents: [], fallback: [], headers: {}, keyList: [], key: { set: true } },
  ];
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers, presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [], onPlugins: ["grok"], movable: ["cursor", "grok", "kiro", "zed"] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = {
  en: { badge: "Deprecated", why: "So that magpie itself isn't banned over them", head: "These built-in subscriptions are deprecated: Cursor, Kiro", later: "Not now" },
  zh: { badge: "已弃用", why: "为防止 magpie 本体因此被封禁", head: "以下内置订阅已弃用：Cursor、Kiro", later: "暂不" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: deprecated built-in subscriptions are badged and explained`, async (t) => {
      const w = L[lang];
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 900, height: 700 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      await page.goto("http://magpie.test/?view=providers");

      // the notice: the signed-in deprecated ones, and why
      const notice = page.locator("#movable .deprecation");
      await notice.waitFor();
      const said = await notice.innerText();
      assert.ok(said.includes(w.head), `the notice doesn't name Cursor and Kiro: ${said}`);
      assert.ok(said.includes(w.why), "the notice doesn't say why");
      assert.ok(!said.includes("Grok"), "names Grok, already on its plugin");

      // the rows: badged with the reason in the tooltip, but not Grok or DeepSeek
      for (const id of ["cursor", "kiro"]) {
        const b = page.locator(`.row.provider[data-id="${id}"] .badge.deprecated`);
        assert.equal((await b.textContent()), w.badge, `${id}'s badge`);
        assert.ok((await b.getAttribute("title")).includes(w.why), `${id}'s badge doesn't say why`);
      }
      for (const id of ["grok", "deepseek"]) assert.equal(await page.locator(`.row.provider[data-id="${id}"] .badge.deprecated`).count(), 0, `${id} is badged`);

      // the Add sheet: Zed (not signed in) badged too, Claude isn't
      await page.locator(".after-list button").first().click();
      await page.locator(".dep-foot").waitFor();
      assert.equal(await page.locator('.tile[data-pick="Zed"] .badge.deprecated').count(), 1, "Zed's tile isn't badged");
      assert.ok((await page.locator('.tile[data-pick="Zed"]').getAttribute("title")).includes(w.why), "Zed's tile doesn't say why");
      assert.equal(await page.locator('.tile[data-pick="Claude"] .badge.deprecated').count(), 0, "Claude's tile is badged");

      // Not now hides the notice, and it stays hidden on the next load
      await page.goto("http://magpie.test/?view=providers");
      await notice.waitFor();
      await notice.locator("button", { hasText: w.later }).click();
      await page.locator("#movable").waitFor({ state: "hidden" });
      await page.goto("http://magpie.test/?view=providers");
      await page.locator('.row.provider[data-id="cursor"] .badge.deprecated').waitFor();
      assert.equal(await page.locator("#movable").isHidden(), true, "the notice came back");
      assert.deepEqual(errors, []);
    });
  }
}
