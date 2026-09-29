# Dropdown browser regression

`menu-scroll.test.cjs` loads the real HTML, CSS and JavaScript with isolated API
fixtures. It checks session folder/model filters and the main model picker in
Chromium and WebKit, including wheel, scrollbar track/thumb, keyboard selection
and dismissal. It does not start the backend or read local user configuration.

`click-scroll.test.cjs` holds the rule that a click never moves the page
(see "where the reader is" in `app.js`): on the Routing page, Live and a day
picked in turn with the list scrolled to its end; a click whose handler sets
scrollTop, grows the page above, redraws itself, or scrolls from a timer or a
shared helper; a control under what it unrolls (`data-unrolls`) going down
with it; a click that asks to go somewhere with `scrollOnPurpose(e)`;
the room kept at the foot going as the reader scrolls back; and the wheel.

`panel-fold.test.cjs` expands and collapses on the Agents page with the list
scrolled to its end, in the tray panel (one agent open) and in the window:
"Show {n} more" unrolls the rest under the button, the view going down with
them and never back up; "Show less", and a row opened and closed, leave what
was clicked where it is on every frame.

`gateway-fold.test.cjs` folds Connect on the Gateway page with the view
scrolled: its fields hide, the head keeps the base URL and a copy button, the
head stays where it was, and the fold is remembered across a reload.

`routing-kind.test.cjs` lists calls Codex makes for itself (a guardian review,
a title, memories, a turn on Luna Reserve, a kind it does not know yet): each
has a grey tag by its model in the Requests list, in English and Chinese, the
model keeping its room first, and the request's story says what it was.

With Node.js and Playwright available:

```sh
node --test internal/gui/tests/menu-scroll.test.cjs internal/gui/tests/click-scroll.test.cjs internal/gui/tests/panel-fold.test.cjs internal/gui/tests/gateway-fold.test.cjs internal/gui/tests/routing-kind.test.cjs
```

If Playwright is installed outside the repository, set `NODE_PATH` to the
directory containing its package. The suite uses Playwright's Chromium and
WebKit binaries (`playwright install chromium webkit`). No frontend dependency
is needed by the app itself. Tested with Playwright 1.63.0.

Set `BROWSER=chromium` or `BROWSER=webkit` for one engine. Set `ARTIFACT_DIR` to
an external directory to retain screenshots and Playwright traces, including
failed assertions. These browser checks run separately from `make test`.
