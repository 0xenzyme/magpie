// Run with Node's test runner and Playwright on the module path; see README.md.
// A provider an OpenCode plugin signs in to: the add sheet lists it under
// "From plugins"; its sign-in asks the way to sign in, the method's
// questions (a pick, then a text the plugin checks), then opens the
// browser and takes the code its page shows; an "api" way takes a key and
// opens the account. Settings → Plugins lists the plugins, why one didn't
// load, and adds one. English and Chinese; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

const plugin = { id: "fakeco", pid: "fakeco", name: "FakeCo", icon: "generic", spec: "opencode-fakeco-auth", signedIn: false, models: 3,
  methods: [{ type: "api", label: "API key" }, { type: "oauth", label: "Browser sign-in" }] };

function server(lang, asked) {
  let signedIn = false;
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    const body = () => route.request().postDataJSON();
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") {
      const providers = [{ id: "openai", name: "OpenAI", icon: "openai", preset: "openai", models: [], agents: [], key: { set: true, masked: "sk-…ab12" } }];
      if (signedIn) providers.push({ id: "fakeco", name: "FakeCo", icon: "", models: [], agents: [], key: {}, account: { agent: "fakeco", agentName: "FakeCo", agentIcon: "generic", user: "API key" } });
      return json({ providers, presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [{ ...plugin, signedIn }] });
    }
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/plugin-signin/prompt") {
      const b = body();
      asked.push(["prompt", b]);
      const inputs = { ...(b.inputs || {}) };
      if (b.key === "team" && !b.value) return json({ error: "Required" });
      if (b.key) inputs[b.key] = b.value;
      if (b.method === 1 && !inputs.where) return json({ prompt: { type: "select", key: "where", message: "Where do you work?", options: [{ label: "Home", value: "home" }, { label: "Work", value: "work" }] }, inputs });
      if (b.method === 1 && inputs.where === "work" && !inputs.team) return json({ prompt: { type: "text", key: "team", message: "Your team", placeholder: "blue" }, inputs });
      return json({ prompt: null, inputs });
    }
    if (url.pathname === "/api/plugin-signin") {
      const b = body();
      asked.push(["signin", b]);
      if (b.key) { signedIn = true; return json({ agent: "fakeco", state: "done", user: "FakeCo" }); }
      return json({ id: "p1", agent: "fakeco", state: "waiting", url: "https://fake.test/auth", pasteCode: true, instructions: "Paste the code FakeCo shows." });
    }
    if (url.pathname === "/api/signin/p1/callback") { asked.push(["code", body()]); return route.fulfill({ status: 204 }); }
    if (url.pathname === "/api/signin/p1") return json({ id: "p1", agent: "fakeco", state: "waiting", url: "https://fake.test/auth", pasteCode: true, instructions: "Paste the code FakeCo shows." });
    if (url.pathname === "/api/plugins") return json({ bun: true, bunVersion: "1.3.0", plugins: [
      { spec: "opencode-fakeco-auth", providers: ["FakeCo"] },
      { spec: "opencode-broken", error: "Cannot find module 'x'", providers: [] },
    ] });
    if (url.pathname === "/api/plugins/add") { asked.push(["add", body()]); return json({ bun: true, plugins: [{ spec: "opencode-fakeco-auth", providers: ["FakeCo"] }, { spec: body().spec, providers: ["Other"] }] }); }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = {
  en: { section: "From plugins", how: "How do you sign in to FakeCo?", next: "Next", code: "Code", finish: "Finish sign-in", key: "FakeCo API key", signIn: "Sign in",
    plugins: "Plugins", failed: /Didn't load: Cannot find module/, signs: "Signs in to FakeCo", add: "Add" },
  zh: { section: "来自插件", how: "用哪种方式登录 FakeCo？", next: "下一步", code: "验证码", finish: "完成登录", key: "FakeCo API Key", signIn: "登录",
    plugins: "插件", failed: /没有加载成功：Cannot find module/, signs: "可登录 FakeCo", add: "添加" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a plugin's provider", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) {
      await t.test(lang, async () => {
        const w = L[lang];
        const page = await (await browser.newContext({ viewport: { width: 900, height: 800 } })).newPage();
        page.setDefaultTimeout(5000);
        const errors = [], asked = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, asked));
        await page.goto("http://magpie.test/?view=providers");
        await page.locator("#addProvider").click();
        const sheet = page.locator("#addSheet");
        await sheet.locator(".kind b", { hasText: w.section }).waitFor();
        const row = sheet.locator('.tile[data-pick="FakeCo"]');
        assert.match(await row.getAttribute("title"), /opencode-fakeco-auth/);

        // the browser way: a pick, a text the plugin checks, then the code
        await row.click();
        const box = sheet.locator(".signing");
        await box.locator(".n", { hasText: w.how }).waitFor();
        if (process.env.ARTIFACT_DIR) await sheet.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `plugin-method-${engine}-${lang}.png`) });
        assert.equal(asked.length, 0, "nothing asked before the way is picked");
        await box.locator("button", { hasText: "Browser sign-in" }).click();
        await box.locator(".n", { hasText: "Where do you work?" }).waitFor();
        await box.locator("button", { hasText: "Work" }).click();
        const team = box.getByRole("textbox", { name: "Your team" });
        await team.waitFor();
        if (process.env.ARTIFACT_DIR) await box.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `plugin-prompt-${engine}-${lang}.png`) });
        await box.locator("button", { hasText: w.next }).click();
        await box.locator(".why", { hasText: "Required" }).waitFor();
        await team.fill("blue");
        await team.press("Enter");
        await box.locator(".s", { hasText: "Paste the code FakeCo shows." }).waitFor();
        const signin = asked.find(([k]) => k === "signin")[1];
        assert.deepEqual(signin, { provider: "fakeco", method: 1, inputs: { where: "work", team: "blue" } });
        if (process.env.ARTIFACT_DIR) await box.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `plugin-code-${engine}-${lang}.png`) });
        const code = box.getByRole("textbox", { name: w.code });
        await code.fill("abc123");
        await box.locator("button", { hasText: w.finish }).click();
        for (let i = 0; i < 50 && !asked.some(([k]) => k === "code"); i++) await page.waitForTimeout(50);
        assert.deepEqual(asked.find(([k]) => k === "code")[1], { url: "abc123" });

        // the key way: the account opens once it's in
        await box.locator("button.text:not(.primary)").last().click();
        await row.click();
        await box.locator("button", { hasText: "API key" }).click();
        const key = box.getByLabel(w.key);
        await key.waitFor();
        assert.equal(await key.getAttribute("type"), "password");
        await key.fill("k1");
        await box.locator("button", { hasText: w.signIn }).click();
        await page.waitForFunction(() => editing === "fakeco");
        assert.deepEqual(asked.filter(([k]) => k === "signin").pop()[1], { provider: "fakeco", method: 0, inputs: {}, key: "k1" });

        // Settings → Plugins
        await page.goto("http://magpie.test/?view=settings");
        const list = page.locator("#pluginsList");
        await list.locator(".sub", { hasText: w.signs }).waitFor();
        await list.locator(".sub.bad", { hasText: w.failed }).waitFor();
        await list.getByRole("textbox").fill("opencode-other-auth");
        await list.locator("button", { hasText: new RegExp("^" + w.add + "$") }).click();
        await list.locator(".name", { hasText: "opencode-other-auth" }).waitFor();
        assert.deepEqual(asked.find(([k]) => k === "add")[1], { spec: "opencode-other-auth" });
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await list.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `plugins-${engine}-${lang}.png`) });
        }
        assert.deepEqual(errors, []);
      });
    }
  });
}
