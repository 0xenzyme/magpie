// Run with Node's test runner and Playwright on the module path; see README.md.
// Cursor Private Inference (#299) takes magpie's gateway only from its
// environment and has nothing else magpie sets, so its row's one control is
// the command that starts it on magpie, in words (Copy launch command), not
// a bare square: one click copies it (posted to /api/copy) and says so. The
// row stays in view among connected agents, as there is nothing set on it to
// tell by. In the tray panel the row itself copies it. The words in Chinese.
// No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const LAUNCH = "CURSOR_LOCAL_AGENT_BASE_URL=http://127.0.0.1:3425/v1 CURSOR_LOCAL_AGENT_API_KEY=magpie-cursor-local '/Applications/Cursor.app/Contents/MacOS/Cursor'";
const models = [{ value: "magpie/deepseek/pro", label: "magpie/deepseek/pro", ref: "deepseek/pro" }];
const agent = (id, name) => ({
  id, name, path: "/test/" + id, wired: true,
  fields: [{ key: "model", label: "model", value: "magpie/deepseek/pro", options: models }],
});
const state = {
  agents: [agent("claude", "Claude Code"), agent("codex", "Codex"), agent("pi", "Pi"),
    { id: "cursor-local", name: "Cursor Private Inference", icon: "cursor", path: "", launch: LAUNCH, fields: [] }],
  profiles: [],
};

function server(lang, copies) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { ...state, settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/copy") { copies.push(req.postDataJSON().text); return route.fulfill({ json: {} }); }
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const row = `.row.agent[data-id="cursor-local"]`;

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": Cursor Private Inference's launch command", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    const open = async (url, lang, copies, viewport = { width: 980, height: 520 }) => {
      const page = await (await browser.newContext({ viewport })).newPage();
      page.setDefaultTimeout(5000);
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang, copies));
      await page.goto(url);
      await page.locator(`.row.agent[data-id="claude"]`).waitFor();
      return page;
    };

    await t.test("the window: the command in words, in view", async () => {
      const copies = [];
      const page = await open("http://magpie.test/", "en", copies);
      const b = page.locator(`${row} .field.launch-solo`);
      await b.waitFor({ state: "visible" });
      assert.equal(await page.locator(`${row} .field.launch`).count(), 0, "no bare square beside it");
      assert.equal((await b.textContent()).trim(), "Copy launch command");
      const title = await b.getAttribute("title");
      assert.match(title, /Cursor Private Inference takes magpie only from its environment/);
      assert(title.includes(LAUNCH), title);
      await b.click();
      await page.waitForTimeout(300);
      assert.deepEqual(copies, [LAUNCH]);
      assert.match(await page.locator("#status").textContent(), /Copied — run it to start Cursor Private Inference on magpie/);
    });

    await t.test("the tray panel: the row copies it", async () => {
      const copies = [];
      const page = await open("http://magpie.test/?mode=panel", "en", copies, { width: 440, height: 560 });
      const sum = page.locator(`${row} .ag-sum`);
      await sum.waitFor({ state: "visible" });
      assert.match(await sum.textContent(), /Copy launch command/);
      await sum.click();
      await page.waitForTimeout(300);
      assert.deepEqual(copies, [LAUNCH]);
    });

    await t.test("in Chinese", async () => {
      const copies = [];
      const page = await open("http://magpie.test/", "zh", copies);
      const b = page.locator(`${row} .field.launch-solo`);
      assert.equal((await b.textContent()).trim(), "复制启动命令");
      await b.click();
      await page.waitForTimeout(300);
      assert.match(await page.locator("#status").textContent(), /已复制，运行它即可让 Cursor Private Inference 走 magpie/);
    });

    assert.deepEqual(errors, []);
  });
}
