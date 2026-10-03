// Run with Node's test runner and Playwright on the module path; see README.md.
// A subscription with several accounts shows one of them on the Usage page
// and in the tray panel — the one that answered last, else the first — the
// others behind "Show N more accounts" (whqtian on Discord: 只显示一个账号
// 即可，其他的可以点击展开); opened is remembered by provider across a
// reload; a provider with one account has no button; the click doesn't move
// the page, and Hide accounts still hides every one. English and Chinese;
// no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const ago = (m) => new Date(Date.now() - m * 60e3).toISOString();
const wb = (user, used, served) => ({ provider: "workbuddy", name: "WorkBuddy", kind: "subscription", icon: "workbuddy-color", plan: "Pro", user, windows: [{ name: "Credits", used, display: `${used * 56} / 5600` }], ...(served ? { lastServedAt: served } : {}) });
const quotas = () => [
  wb("alpha@example.com", 10),
  wb("bravo@example.com", 20, ago(30)),
  wb("charlie@example.com", 30, ago(2)), // answered last: the one in sight
  wb("delta@example.com", 40),
  { provider: "zcode", name: "ZCode", kind: "subscription", icon: "zcode", plan: "Start", user: "solo@example.com", windows: [{ name: "5 hours", used: 10 }] },
  { provider: "codex", name: "Codex", kind: "subscription", icon: "openai", plan: "Plus", user: "one@example.com", windows: [{ name: "5 hours", used: 20 }] },
  { provider: "codex", name: "Codex", kind: "subscription", icon: "openai", plan: "Plus", user: "two@example.com", windows: [{ name: "5 hours", used: 50 }] },
];

function serve(lang, panel) {
  const settings = { theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd" };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:${!panel}};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") return json(quotas());
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const words = {
  en: { more3: "Show 3 more accounts", more1: "Show 1 more account", fewer: "Show fewer accounts", hide: "Hide accounts" },
  zh: { more3: "展开其余 3 个账号", more1: "展开其余 1 个账号", fewer: "收起其余账号", hide: "账号打码" },
};

// what each card shows: its name, the accounts in sight, its button
const cards = (page) => page.evaluate(() => [...document.querySelectorAll("#subscriptionUsage > .subscription-card")].map((c) => ({
  name: c.querySelector(".subscription-head b").textContent,
  users: [...c.querySelectorAll(".subscription-account")].filter((a) => a.offsetParent).map((a) => a.querySelector(".user").title),
  meters: [...c.querySelectorAll(".quota-windows")].filter((a) => a.offsetParent).length,
  more: c.querySelector(".quota-more")?.textContent ?? null,
})));

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    test(`${engine} ${lang}: several accounts show one, the others behind a button`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const context = await browser.newContext({ viewport: { width: 900, height: 300 }, reducedMotion: "reduce" });
      const errors = [];
      const open = async (url) => {
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, url.includes("mode=panel")));
        await page.goto(url);
        return page;
      };
      const page = await open("http://magpie.test/?view=usage");
      await page.evaluate(() => { localStorage.removeItem("magpie.usageOpen"); localStorage.removeItem("magpie.maskEmails"); });
      await page.reload();
      await page.waitForSelector(".subscription-account .user", { state: "attached" });

      let got = await cards(page);
      const by = (name) => got.find((c) => c.name === name);
      assert.deepEqual(by("WorkBuddy"), { name: "WorkBuddy", users: ["charlie@example.com"], meters: 1, more: w.more3 });
      assert.deepEqual(by("Codex"), { name: "Codex", users: ["one@example.com"], meters: 1, more: w.more1 });
      assert.equal(by("ZCode").more, null, "one account: no button");
      assert.deepEqual(by("ZCode").users, ["solo@example.com"]);

      // opening it doesn't move the page
      const view = page.locator("#view-usage");
      await page.mouse.move(450, 200);
      await page.mouse.wheel(0, 60);
      await page.waitForFunction(() => document.querySelector("#view-usage").scrollTop > 0);
      await page.waitForTimeout(300);
      const head = page.locator("#subscriptionUsage > [data-key=workbuddy] .subscription-head");
      const before = await view.evaluate((v) => v.scrollTop);
      assert.ok(before > 0, "the view was scrolled");
      const headAt = await head.evaluate((e) => e.getBoundingClientRect().top);
      await page.locator("#subscriptionUsage > [data-key=workbuddy] .quota-more").click();
      await page.waitForTimeout(700);
      assert.equal(await view.evaluate((v) => v.scrollTop), before, "the view didn't scroll");
      assert.equal(await head.evaluate((e) => e.getBoundingClientRect().top), headAt, "the card stays where it was");
      got = await cards(page);
      assert.deepEqual(by("WorkBuddy"), { name: "WorkBuddy", users: ["alpha@example.com", "bravo@example.com", "charlie@example.com", "delta@example.com"], meters: 4, more: w.fewer });
      assert.equal(by("Codex").more, w.more1, "another provider stays folded");

      // remembered across a reload, by provider
      await page.reload();
      await page.waitForSelector(".subscription-account .user", { state: "attached" });
      got = await cards(page);
      assert.equal(by("WorkBuddy").users.length, 4);
      assert.equal(by("Codex").users.length, 1);

      // Hide accounts still hides every account, the folded ones too
      await page.locator("#usageMask").click();
      await page.waitForFunction(() => document.querySelectorAll("#subscriptionUsage .subscription-account .user .pii").length === 7);
      assert.ok(!/example\.com/.test(await page.locator("#subscriptionUsage").innerText()));
      await page.locator("#usageMask").click();

      // the tray panel folds the same way, and follows what was opened
      const panel = await open("http://magpie.test/?mode=panel");
      await panel.setViewportSize({ width: 380, height: 900 });
      await panel.evaluate(() => setPanelTab("usage"));
      await panel.waitForSelector("#panelQuota .pq-card .pq-user");
      const pq = () => panel.evaluate(() => Object.fromEntries([...document.querySelectorAll("#panelQuota .pq-group")].map((g) => [g.querySelector(".pq-gn").textContent, { users: [...g.querySelectorAll(".pq-user")].map((u) => u.textContent), more: g.querySelector(".pq-more")?.textContent ?? null }])));
      let p = await pq();
      assert.deepEqual(p.WorkBuddy, { users: ["alpha@example.com", "bravo@example.com", "charlie@example.com", "delta@example.com"], more: w.fewer });
      assert.deepEqual(p.Codex, { users: ["one@example.com"], more: w.more1 });
      assert.equal(p.ZCode.more, null);
      await panel.locator("#panelQuota .pq-more", { hasText: w.fewer }).click();
      p = await pq();
      assert.deepEqual(p.WorkBuddy, { users: ["charlie@example.com"], more: w.more3 });
      // and the window follows the panel
      await page.waitForFunction(() => document.querySelector("#subscriptionUsage > [data-key=workbuddy] .quota-more")?.getAttribute("aria-expanded") === "false");
      assert.deepEqual(errors, []);
    });
  }
}
