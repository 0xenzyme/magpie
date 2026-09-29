// Run with Node's test runner and Playwright on the module path; see README.md.
// The Settings page's warm-ups and check-in, grouped by service (#124
// Mrhe525: a long flat list): after the Preferences list, a Codex group
// (Warm up on reset, Daily warm-up), a Claude Code group (the same two) and
// a WorkBuddy group (Daily check-in), each under its own heading as Local
// network is, the rows' names not saying the service again, the lines under
// them short, with how the last warm-up and today's check-in went still on
// them. The WorkBuddy group is there only with an account signed in; a
// daily warm-up's time field only while it is on. Every control posts the
// same setting it did, and a click on one with the page scrolled down moves
// nothing. English and Chinese; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const today = new Date(Date.now() + 8 * 3600e3).toISOString().slice(0, 10);

function settingsPayload(over) {
  return {
    theme: "light", lang: "en", tray: "panel", quotaLeft: false, currency: "usd",
    dock: false, dockWindow: false, proxy: "", redact: false, redactPersonal: false, redactWords: [],
    codexWarmup: "", claudeWarmup: "", codexWarmAt: "", claudeWarmAt: "", workbuddyCheckin: false, noStats: false,
    trayUsage: "", trayUsageEvery: 3, vision: "", imageGen: "",
    version: "0.1.400", dir: "~/.config/magpie", gateway: "http://127.0.0.1:3425",
    proxyNow: "none", proxySource: "none", login: false,
    visionModels: [], imageGenModels: [], lanURLs: [],
    fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
    ...over,
  };
}

function server(lang, posts, over) {
  let cur = settingsPayload({ lang, ...over });
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" }, fx: cur.fx });
    if (url.pathname === "/api/settings") {
      if (req.method() === "POST") {
        const body = req.postDataJSON();
        posts.push(body);
        cur = { ...cur, ...body };
      }
      return json(cur);
    }
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const L = {
  en: {
    prefs: "Preferences", lan: "Local network", reset: "Warm up on reset", daily: "Daily warm-up", checkin: "Daily check-in",
    weekly: "Weekly", both: "Weekly and 5-hour", on: "On", off: "Off", started: "last started", checked: "Ann checked in today +2, 3-day streak",
  },
  zh: {
    prefs: "偏好", lan: "局域网", reset: "窗口重置时预热", daily: "每日定时预热", checkin: "每日自动签到",
    weekly: "每周", both: "每周和 5 小时", on: "开启", off: "关闭", started: "上次启动于", checked: "Ann 今日已签到 +2, 连续 3 天",
  },
};

const view = (page) => page.locator("#view-settings").evaluate((v) => v.scrollTop);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": warm-ups and check-in grouped by service", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const pages = [];
    t.after(async () => {
      if (process.env.ARTIFACT_DIR) {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        for (const [i, p] of pages.entries()) await p.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-settings-groups-${i}.png`) });
      }
      await browser.close();
    });
    const open = async (lang, posts, over) => {
      const errors = [];
      const page = await (await browser.newContext({ viewport: { width: 900, height: 480 }, reducedMotion: "reduce" })).newPage();
      pages.push(page);
      page.setDefaultTimeout(5000);
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang, posts, over));
      await page.goto("http://magpie.test/");
      await page.locator("#prefs").click();
      await page.locator("#warmSegs .opt").first().waitFor();
      return { page, errors };
    };

    for (const lang of ["en", "zh"]) {
      const w = L[lang];
      await t.test(lang, async () => {
        const posts = [];
        const { page, errors } = await open(lang, posts, {
          codexWarmed: new Date(Date.now() - 3600e3).toISOString(),
          workbuddy: true,
          workbuddyCheckins: [{ user: "Ann", day: today, at: new Date().toISOString(), outcome: "claimed", credit: 2, streak: 3 }],
        });

        // the headings, in order: the services' groups after Preferences,
        // before Local network; each group's rows, named without the service
        const heads = await page.locator("#view-settings > .row-head:not([hidden]) .label").allTextContents();
        const at = heads.indexOf(w.prefs);
        assert(at >= 0, `Preferences heading: ${heads}`);
        assert.deepEqual(heads.slice(at, at + 5), [w.prefs, "Codex", "Claude Code", "WorkBuddy", w.lan]);
        const rows = (list) => page.locator(`#${list} .row.pref:not([hidden]) .who .name`).allTextContents();
        assert.deepEqual(await rows("codexWarmList"), [w.reset, w.daily]);
        assert.deepEqual(await rows("claudeWarmList"), [w.reset, w.daily]);
        assert.deepEqual(await rows("wbList"), [w.checkin]);
        const main = await page.locator("#view-settings .list.prefs").first().locator(".who .name").allTextContents();
        for (const id of ["#warmSegs", "#warmAtSegs", "#claudeWarmSegs", "#claudeWarmAtSegs", "#wbCheckinSegs"]) {
          assert.equal(await page.locator("#view-settings .list.prefs").first().locator(id).count(), 0, id + " is out of the Preferences list");
        }
        assert(!main.some((n) => /Codex|Claude|WorkBuddy/.test(n)), `no service rows left in Preferences: ${main}`);

        // short lines, the status still on them
        const warmSub = await page.locator("#warmSub").textContent();
        assert(warmSub.includes(w.started), `the last warm-up is shown: ${warmSub}`);
        assert((await page.locator("#warmAtSub").textContent()).length < 60, "the daily line is short");
        const wbSub = await page.locator("#wbCheckinSub").textContent();
        assert(wbSub.includes(w.checked), `today's check-in is shown: ${wbSub}`);
        // no coloured stripe down a group's or a row's left side
        const stripes = await page.evaluate(() => [...document.querySelectorAll("#codexWarmList, #claudeWarmList, #wbList, #codexWarmList .row, #claudeWarmList .row, #wbList .row")]
          .filter((e) => { const c = getComputedStyle(e); return c.borderLeftWidth !== c.borderRightWidth || c.borderLeftColor !== c.borderRightColor; }).length);
        assert.equal(stripes, 0, "no left-border accents");

        // no time field while a daily warm-up is off
        assert.equal(await page.locator("#warmAtSegs input.at").count(), 0);
        assert.equal(await page.locator("#claudeWarmAtSegs input.at").count(), 0);

        // scrolled down (a real wheel) till the Codex group is at the top,
        // all three groups in sight, each control posts its setting and the
        // page stays where it is
        await page.setViewportSize({ width: 900, height: 720 });
        await page.locator("#codexWarmList").hover();
        const headTop = () => page.evaluate(() => document.querySelector("#codexWarmList").getBoundingClientRect().top - document.querySelector("#view-settings").getBoundingClientRect().top);
        for (let i = 0; i < 60 && (await headTop()) > 40; i++) { await page.mouse.wheel(0, 40); await page.waitForTimeout(15); }
        await page.waitForTimeout(300);
        assert(await page.evaluate(() => { const v = document.querySelector("#view-settings").getBoundingClientRect(), r = document.querySelector("#wbList").getBoundingClientRect(); return r.bottom <= v.bottom; }), "the groups are in sight");
        const before = await view(page);
        assert(before > 0, "the settings page must be long enough to scroll");
        const last = async (key, want) => {
          for (let i = 0; i < 50 && !(posts.length && JSON.stringify(posts.at(-1)[key]) === JSON.stringify(want)); i++) await page.waitForTimeout(40);
          assert.deepEqual(posts.at(-1)[key], want, key);
          await page.waitForTimeout(250);
          assert.equal(await view(page), before, key + ": the click must not scroll the page");
        };
        const pick = (box, name) => page.locator(`${box} .opt`, { hasText: name }).first().click();

        await page.locator("#warmSegs .opt").nth(1).click();
        await last("codexWarmup", "week");
        await page.locator("#claudeWarmSegs .opt").nth(2).click();
        await last("claudeWarmup", "all");
        await pick("#warmAtSegs", w.on);
        await last("codexWarmAt", "06:00");
        const field = page.locator("#warmAtSegs input.at");
        await field.waitFor();
        assert.equal(await field.inputValue(), "06:00", "the time field comes with the daily warm-up on");
        await field.fill("07:30");
        await field.dispatchEvent("change");
        await last("codexWarmAt", "07:30");
        await pick("#claudeWarmAtSegs", w.on);
        await last("claudeWarmAt", "06:00");
        await page.locator("#claudeWarmAtSegs input.at").waitFor();
        await pick("#warmAtSegs", w.off);
        await last("codexWarmAt", "");
        await page.waitForFunction(() => !document.querySelector("#warmAtSegs input.at"));
        await pick("#wbCheckinSegs", w.on);
        await last("workbuddyCheckin", true);
        // the others kept as they were set
        assert.equal(posts.at(-1).codexWarmup, "week");
        assert.equal(posts.at(-1).claudeWarmup, "all");
        assert.equal(posts.at(-1).claudeWarmAt, "06:00");
        assert.deepEqual(errors, []);
      });

      await t.test(lang + ": no WorkBuddy signed in", async () => {
        const { page, errors } = await open(lang, [], {});
        assert.equal(await page.locator("#wbHead").isVisible(), false, "no WorkBuddy heading");
        assert.equal(await page.locator("#wbList").isVisible(), false, "no WorkBuddy group");
        assert(await page.locator("#codexWarmList").isVisible());
        assert(await page.locator("#claudeWarmList").isVisible());
        assert.deepEqual(errors, []);
      });
    }
  });
}
