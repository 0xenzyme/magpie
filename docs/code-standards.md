# Code standards

These rules come from what PR reviews keep asking for. A reviewer checks
each one, and a PR that doesn't meet one gets a comment saying which. How to
describe and review a change to a subsystem is in the
[subsystem references](subsystems/README.md).

## What a change contains

| Rule | What a reviewer looks at |
| --- | --- |
| A change does only what its goal needs. | Every change in behavior has a reason. Unrelated refactors, extra features and policy changes go in a PR of their own. |
| A fix covers exactly the cases it means to. | Which cases change, and which neighboring ones stay the same. Different errors are not handled as one. |
| A change holds on the whole path. | Follow it from the user's input to the end result, through the callers, the state it changes and the side effects. One correct function doesn't make the whole operation correct. For example, an agent's model picks are saved under one id and read under another (#926). An update button doesn't update the binary that actually runs (#930). |
| Checks cover the edges the change touches. | For numbers, check the boundaries. For paths, check each platform (macOS, Linux, Windows). For concurrency, check races and stale state. Pick checks by risk. |

## Tests

| Rule | What a reviewer looks at |
| --- | --- |
| A fix comes with a test that fails without it. | The reviewer puts the old code back and runs the test, and the test must fail. A PR description says what it failed with. |
| Tests use real inputs. | Fixtures look like real requests, real serialized output and real files on disk. That includes missing fields, empty values and defaults. |
| Tests check behavior, not implementation. | Assert what the user or caller needs. Formatting changes and internal refactors shouldn't break a test, unless exact bytes are the requirement. |
| Tests stay out of the real machine. | Run them under a temp HOME with Go's caches pinned (see the snippet in [provider-plugins.md](subsystems/provider-plugins.md)). Never touch a real agent's config or `~/.config/magpie`. An agent's variable goes in `agentenv.Vars`, so the sandbox clears it. |

Before a merge, run `go vet`, `go build`, `GOOS=linux go build -tags nogui`
and `GOOS=windows go build`, plus `go test -tags nogui` for the packages the
change touches. For GUI changes, also run the Playwright tests in
`internal/gui/tests` in Chromium and WebKit. A review lists any check it
didn't run.

## Go

- Write `any`, not `interface{}`.
- Read environment variables that name a folder with `appdir.Getenv`, which ignores a relative path. A variable in `agentenv.Vars` that names no folder goes in `agentenv.NotPaths`.
- A comment says what the code is for, in a sentence or two. Name functions instead of giving line numbers, which go stale.

## GUI

- Every string the user sees goes through `t()` and gets its translations in `i18n.js`. Tests run in Chinese and English.
- Use no native `<select>`. Dropdowns use the app's own menu (`openProtoMenu`).
- A click never scrolls the page (`scrollOnPurpose`).
- Don't use colored left-border stripes. Mark state with a dot or a swatch.

## Commits

A commit message names the areas it changes, then says what the user now
sees, written as plain behavior: `gateway: a 429 that says the balance is
spent is out of credit, not a rate limit`. Give the issue number or the
reporter. The body says how it was verified, and which test fails without
the change.
