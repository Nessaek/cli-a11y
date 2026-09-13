# cli-a11y

Runs a command-line tool under a range of terminal conditions and grades how usable the result is for someone on a screen reader, a magnifier, a high-contrast theme, voice control or switch access.

```bash
cli-a11y -- git status
```

It reports findings by severity, exits non-zero when anything serious turns up, and takes `--json` for CI.

## Why

The failures are not exotic — a red `FAILED` with no word attached, a spinner that a screen reader re-announces forty times a second, faint grey hints at 3:1 contrast, a prompt with no `--yes`. Every one of those is mechanically detectable.

People have done this work, but by hand. [GitHub rebuilt the `gh` prompts](https://github.blog/engineering/user-experience/building-a-more-accessible-github-cli/) on an accessible prompting library, swapped spinners for static progress text and moved to a customisable 4-bit palette; `gcloud` has an `accessibility/screen_reader` property that does much the same. Neither shipped a checker. Search for "CLI accessibility tool" and you get web auditors that happen to run in a terminal — pa11y, axe-core — which is the inverse of this.

What does exist, and is worth knowing about, is a family of PTY test harnesses: [termlens](https://github.com/vyncint/termlens), [microsoft/tui-test](https://github.com/microsoft/tui-test), `ratatui-testlib`. They spawn a binary on a real pty and render its output through a proper VT emulator so you can assert on the screen a user would see. None of them carries accessibility rules — they are the layer underneath this, and a more rigorous one than the terminal emulation here. See [What it can't do](#what-it-cant-do).

The thing that decided the design was realising that piping a CLI's output tells you almost nothing. Most tools suppress colour, spinners and prompts the moment stdout stops being a terminal, so a pipe-based checker grades output that no terminal user ever sees. The faults live on the terminal path specifically, which means the checker has to run the tool on a real pty.

## Running it

Node 18+. No dependencies, no build step.

```bash
node bin/cli-a11y -- your-tool
```

`--help` and `--version` alone are a shallow audit. Progress bars, prompts and status colours live in the real work, so point it at some:

```bash
cli-a11y --cmd "status" --cmd "log --oneline -5" -- git
```

Each `--cmd` runs the tool for real. Don't point it at anything destructive.

Useful flags:

| | |
|---|---|
| `--cmd "<args>"` | Extra invocation to exercise. Repeatable. |
| `--fail-on <level>` | `critical`, `serious` (default), `moderate`, `minor`, `none`. |
| `--json` | Full report as JSON. |
| `--quiet` | Failures only, without the roll-call of what passed. |
| `--timeout <ms>` | Per-probe timeout, default 10000. |

Exit codes: `0` clean at your threshold, `1` findings, `2` couldn't run the target.

## How it works

Every check reads from a fixed set of recorded runs. Nothing spawns the target on its own, so the rule set can grow without the audit getting slower.

**The probe matrix.** Each invocation is run as a family: on a terminal, through a pipe, with `NO_COLOR=1`, under `TERM=dumb`, at 40 columns and at 100. Most of the interesting questions are comparisons — "does it honour `NO_COLOR`" means nothing except against the same command without it.

**Getting a pty without a native dependency.** `script(1)` is on every macOS and Linux box, and `script -q /dev/null <cmd>` hands the target a real terminal. Three things had to be worked out the hard way. `script` calls `tcgetattr` on its own stdin and dies on the socketpair Node hands out for `stdio: 'pipe'` — so every pty run gets a real file descriptor instead. It rejects a FIFO for the same reason but accepts a shell pipe, which is the only way to hold stdin open and silent so a CLI that waits for input really waits. And a pty's size can't be set from outside, but `stty cols 40` *inside* the session resizes it, so the width probes are genuine rather than an exported `$COLUMNS` the target is free to ignore.

**Reading the output.** A hand-written SGR parser resolves every colour to RGB — the 16 named colours, the 256-colour cube, truecolour — and computes WCAG contrast. It also resolves bold-plus-basic-colour to the bright variant, because that is what terminals actually draw, and blends faint text halfway into the background for the same reason. Carriage returns are replayed as overwrites, so a spinner measures as the eight columns the user sees rather than the nine hundred bytes it wrote.

**Scoring.** Each finding carries a severity weight off 100, per dimension and overall. It's a summary, not the point; the findings are the point.

## What it checks

**Vision and screen readers.** Colour written to a pipe. `NO_COLOR` and `TERM=dumb` ignored. Contrast below WCAG AA, measured against both a dark and a light terminal. Faint text used for content. Colour as the only signal. In-place redrawing, measured as repaints per second. A terminal left with the cursor hidden or the alternate screen stuck. Box-drawing and braille glyphs with no ASCII fallback. Output that overflows 40 columns, or that ignores the terminal width entirely.

**Interaction and input.** Prompting with no documented way to answer up front. Prompting even when stdin is not a terminal, which nothing can drive. Arrow-key menus, which a screen reader cannot follow because nothing is announced when the highlight moves. Prompts on a countdown. No machine-readable output mode — scripting is how many disabled users avoid a difficult interface altogether.

**Comprehension.** Help that doesn't exist, goes to stderr, or exits non-zero. Bad input that exits 0. Errors on the wrong stream, or that neither name the input they rejected nor say where to look next. Help with no usage line, no options section, no examples. Long lines and long sentences.

## Does it actually work

`npm test` pins every rule to two fixtures: `test/fixtures/bad-cli.js`, which commits each fault deliberately, and `test/fixtures/good-cli.js`, which is the same tool built the other way round. A rule has to fire on one and clear on the other. A rule that can't tell them apart is worse than no rule, because it spends the reader's attention on nothing.

The suite also audits `cli-a11y` itself and fails if it doesn't come back clean. The report honours `NO_COLOR`, pairs every colour with a word, prints one line per probe rather than redrawing, and uses no faint text anywhere.

On real tools it lands where you'd expect. `git` scores 85: it ignores the terminal width, overflows at 40 columns, and its top-level help lists flags only inside the usage synopsis with no navigable options section. `bad-cli.js` scores 0 with 26 findings.

The sharpest test available is `gcloud`, because Google ships a documented accessibility mode and we can grade the same binary with it off and on:

```bash
cli-a11y --cmd "components list" -- gcloud                      # 81/100
CLOUDSDK_ACCESSIBILITY_SCREEN_READER=1 cli-a11y ... -- gcloud   # 85/100
```

The single finding that clears is `V-GLYPHS` — 679 box-drawing characters become zero, which is exactly the flattened-table behaviour Google documents. Nothing else moves, and that is the interesting part: with accessibility mode on, `gcloud` still ignores the terminal width, still overflows at 40 columns, and repaints *more* in place rather than less (carriage returns go from 57 to 212). A documented accessibility mode is not the same as an accessible tool, and the checks can tell the difference.

Pointing it at real tools is also how the rules get fixed. `gcloud` alone exposed five false positives — a paged help counted as a hang, a pager's bold and underline counted as ignoring `NO_COLOR`, `--quiet` not recognised as a non-interactive flag, a killed pager blamed for leaving the terminal dirty, and an error message that says "run: gcloud help" not counted as pointing anywhere. All five are fixed and pinned by tests.

## What it can't do

**It can't tell you what a screen reader says.** It reads the escape sequences and infers. A tool that repaints a line 40 times a second is a problem for NVDA, JAWS, VoiceOver and Orca in different ways and to different degrees, and the finding flattens that.

**Contrast is an estimate.** Terminals re-theme the ANSI palette freely, and the real background is unknowable, so contrast is computed against a typical dark and a typical light terminal. A colour that fails both is unarguable; one that fails a single background is a statement about a theme assumption, which is why it scores lower.

**Colour-only and prompt detection are heuristics.** A coloured run counts as self-labelling if it looks like a status token — `[ok]`, `DOWN`, `✗` — and as colour-only if it looks like a value. Hostnames and status words don't always sort themselves cleanly. Prompt detection reads the tail of the output and the fact that the run didn't terminate.

**The comprehension checks are proxies.** Sentence length and the presence of an `Options:` heading are not comprehension, and a tool can score well on all of them while still being baffling.

**The terminal emulation is crude.** Output is parsed for escape sequences and carriage returns are replayed as overwrites, which is enough to measure colour, repaint churn and line width in the common case. It is not a VT emulator: cursor addressing, scroll regions and soft wrapping are counted rather than simulated, so a full-screen TUI is measured less accurately than a line-oriented tool. A harness like termlens models the screen properly, at the cost of a dependency and a runtime.

**No pty means a shallow audit.** Where `script(1)` isn't usable, every terminal probe is skipped and reported as not-checked rather than passed, and the report says so at the top.

The largest real gap is that a clean report is not a claim of accessibility. Everything here is mechanically checkable, and mechanically checkable faults are a minority of the ones that matter. It finds the floor, not the ceiling.
