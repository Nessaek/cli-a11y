'use strict';

// Checks for people who use a screen reader, a magnifier, a high-contrast
// theme, or simply cannot pick a red "FAILED" out of a wall of white text.

const ansi = require('../ansi');
const U = require('./util');

const DIM = 'vision';

const SEMANTIC = new Set([1, 2, 3, 9, 10, 11]); // red, green, yellow and their bright forms

function nearestIndex(rgb) {
  let best = 0;
  let bestD = Infinity;
  ansi.BASE16.forEach((c, i) => {
    const d = (c[0] - rgb[0]) ** 2 + (c[1] - rgb[1]) ** 2 + (c[2] - rgb[2]) ** 2;
    if (d < bestD) { bestD = d; best = i; }
  });
  return best;
}

// Terminals draw SGR 2 (faint) by blending the colour halfway into the
// background. The named colour is not what lands on the screen.
function applyDim(fg, bg) {
  return [0, 1, 2].map((i) => Math.round(fg[i] * 0.5 + bg[i] * 0.5));
}

// Faint text that sets no colour of its own is still a contrast problem — the
// most common one — so the terminal's default foreground stands in for it.
const DEFAULT_FG_ON_DARK = [204, 204, 204];
const DEFAULT_FG_ON_LIGHT = [0, 0, 0];

function defaultFgFor(bg) {
  return ansi.luminance(bg) > 0.5 ? DEFAULT_FG_ON_LIGHT : DEFAULT_FG_ON_DARK;
}

/**
 * Every styled run across all pty probes, carrying the probe it came from and
 * the full plain-text line it sits on — a run only means something in the
 * context of the line a reader would hear.
 */
function styledRuns(probes) {
  const out = [];
  for (const [id, p] of Object.entries(probes)) {
    if (id === '_meta' || !usableForColour(p) || !p.tty) continue;
    const parsed = ansi.parse(p.output);
    const lines = parsed.plain.split('\n').map(U.renderLine);
    for (const r of parsed.runs) {
      if (!r.text.trim()) continue;
      if (!r.style.fg && !r.style.bg && !r.style.dim && !r.style.reverse) continue;
      out.push({ ...r, probe: id, contextLine: lines[r.line] ?? r.text });
    }
  }
  return out;
}

/**
 * Nothing here can be judged without terminal output. Where no pty was
 * available the honest answer is "not checked" — reporting a pass for a probe
 * that never ran is the one thing an audit tool must never do.
 */
function needsTty(probes, id, title) {
  const any = Object.entries(probes)
    .some(([k, pr]) => k !== '_meta' && U.ok(pr) && pr.tty);
  if (any) return null;
  return [U.skip({
    id,
    dimension: DIM,
    title,
    detail: 'Not checked: no terminal probe was possible on this machine, so there was no terminal output to inspect.',
  })];
}

function checkContrast(probes) {
  const gap = needsTty(probes, 'V-CONTRAST', 'Colour contrast');
  if (gap) return gap;
  const runs = styledRuns(probes);
  if (!runs.length) {
    return [U.pass({
      id: 'V-CONTRAST', dimension: DIM, title: 'Colour contrast',
      detail: 'No coloured output was produced, so there is nothing that can fail contrast.',
    })];
  }

  // One entry per distinct colour pairing, so a colour used on 400 lines is
  // reported once.
  const seen = new Map();
  for (const r of runs) {
    // Either an explicit colour, or faint text taking the default one.
    if (!r.style.fg && !r.style.dim) continue;
    const key = `${r.style.fgName}|${r.style.bgName}|${r.style.dim ? 'dim' : ''}`;
    if (!seen.has(key)) seen.set(key, { ...r, count: 0 });
    seen.get(key).count++;
  }

  const failures = [];
  for (const r of seen.values()) {
    const explicitBg = r.style.bg;
    const backgrounds = explicitBg
      ? [['the background it sets itself', explicitBg]]
      : [['a dark terminal', ansi.DARK_BG], ['a light terminal', ansi.LIGHT_BG]];

    const ratios = backgrounds.map(([label, bg]) => {
      const fg = r.style.fg || defaultFgFor(bg);
      const effective = r.style.dim ? applyDim(fg, bg) : fg;
      return { label, bg, ratio: ansi.contrast(effective, bg), effective };
    });

    const bad = ratios.filter((x) => x.ratio < 4.5);
    if (!bad.length) continue;
    // The friendlier of the two backgrounds is what decides how bad this is:
    // a colour that works on a dark terminal is merely a theme assumption, one
    // that works on neither cannot be read at all.
    const best = Math.max(...ratios.map((x) => x.ratio));
    const tier = bad.length < ratios.length ? 'minor' : (best < 3 ? 'serious' : 'moderate');
    failures.push({ run: r, ratios, bad, best, tier, everywhere: bad.length === ratios.length });
  }

  if (!failures.length) {
    return [U.pass({
      id: 'V-CONTRAST', dimension: DIM, title: 'Colour contrast',
      detail: `All ${seen.size} colour combinations reach WCAG AA (4.5:1).`,
    })];
  }

  const RANK = { serious: 0, moderate: 1, minor: 2 };
  const severity = failures.map((f) => f.tier).sort((a, b) => RANK[a] - RANK[b])[0];
  const unreadable = failures.filter((f) => f.tier === 'serious');
  const bothWays = failures.filter((f) => f.everywhere);

  const evidence = failures
    .sort((a, b) => RANK[a.tier] - RANK[b.tier])
    .slice(0, 8)
    .map((f) => {
      const where = f.ratios.map((x) => `${x.ratio.toFixed(2)}:1 on ${x.label}`).join(', ');
      const base = f.run.style.fg ? ansi.describe(f.run.style.fg) : 'the default foreground';
      const desc = base + (f.run.style.dim ? ' (faint)' : '');
      return `${desc} — ${where} — e.g. ${JSON.stringify(U.excerpt(f.run.text, 40))}`;
    });

  const parts = [`${failures.length} colour combination(s) fall below WCAG AA (4.5:1).`];
  if (unreadable.length) {
    parts.push(`${unreadable.length} of them stay under 3:1 whichever background the user has, which is below the threshold for even large text.`);
  } else if (bothWays.length) {
    parts.push(`${bothWays.length} fall short on both a dark and a light terminal, though not severely.`);
  } else {
    parts.push('Each works on one of the two common backgrounds and not the other, and the CLI cannot know which the user has.');
  }

  return [U.fail({
    id: 'V-CONTRAST',
    dimension: DIM,
    title: 'Colour contrast below WCAG AA',
    severity,
    detail: parts.join(' '),
    evidence,
    remedy: 'Prefer the bright variants of the ANSI colours, or pair colour with bold. Never rely on faint (SGR 2) for anything that must be read.',
  })];
}

function checkDim(probes) {
  const gap = needsTty(probes, 'V-DIM', 'Faint text');
  if (gap) return gap;
  const dimRuns = styledRuns(probes).filter((r) => r.style.dim && r.text.trim().length > 2);
  if (!dimRuns.length) {
    return [U.pass({
      id: 'V-DIM', dimension: DIM, title: 'Faint text',
      detail: 'No faint (SGR 2) text was used.',
    })];
  }
  return [U.fail({
    id: 'V-DIM',
    dimension: DIM,
    title: 'Faint text used for real content',
    severity: 'moderate',
    detail: `${dimRuns.length} run(s) use faint styling, which most terminals render by blending the text halfway into the background. It is the single most common cause of unreadable CLI output for low-vision users.`,
    evidence: dimRuns.slice(0, 5).map((r) => `${r.probe}: ${JSON.stringify(U.excerpt(r.text, 50))}`),
    remedy: 'Use faint only for decoration that repeats information available elsewhere. Never for hints, paths, defaults or timings the user has to read.',
  })];
}

// A probe sitting in a pager is not showing us the CLI's own output any more:
// less draws its own styling, and we killed the process mid-session. Nothing
// about colour can be concluded from it.
function usableForColour(p) {
  return U.ok(p) && !U.isPaging(p);
}

/** Families whose plain terminal run actually emitted colour. */
function colouredFamilies(probes) {
  return (probes._meta.families || []).filter((f) => {
    const p = probes[f.tty];
    return usableForColour(p) && ansi.hasColour(p.raw);
  });
}

function styleCount(p) {
  return (p.raw.match(/\x1b\[[0-9;]*m/g) || []).length;
}

function checkPipeColour(probes) {
  const offenders = (probes._meta.families || [])
    .map((f) => probes[f.pipe])
    .filter((p) => U.ok(p) && ansi.hasColour(p.raw));

  if (!offenders.length) {
    return [U.pass({
      id: 'V-PIPE-COLOUR', dimension: DIM, title: 'Colour suppressed when piped',
      detail: 'No escape sequences when stdout is not a terminal.',
    })];
  }
  return [U.fail({
    id: 'V-PIPE-COLOUR',
    dimension: DIM,
    title: 'Colour written to a pipe',
    severity: 'serious',
    detail: 'Escape sequences appear when stdout is not a terminal. Anything that redirects this output — a log file, a pager, a screen reader reading a saved transcript, a CI artefact — gets the raw control codes as literal text.',
    evidence: offenders.slice(0, 3).map((p) => `${p.id}: ${styleCount(p)} styling sequences with stdout piped`),
    remedy: 'Gate colour on isatty(stdout), as almost every colour library does by default.',
  })];
}

/** NO_COLOR and TERM=dumb are the same check against a different switch. */
function checkColourSwitch(probes, { id, key, title, label, severity, detail, remedy }) {
  const coloured = colouredFamilies(probes);
  if (!coloured.length) {
    return [U.skip({
      id, dimension: DIM, title,
      detail: 'The CLI emits no colour on a terminal either, so there is nothing to turn off.',
    })];
  }
  const offenders = coloured
    .map((f) => probes[f[key]])
    .filter((p) => usableForColour(p) && ansi.hasColour(p.raw));

  if (!offenders.length) {
    return [U.pass({
      id, dimension: DIM, title,
      detail: `Setting ${label} removed all styling.`,
    })];
  }
  return [U.fail({
    id, dimension: DIM, title: `${label} ignored`, severity, detail,
    evidence: offenders.slice(0, 3).map((p) => `${p.id}: still emitted ${styleCount(p)} styling sequences`),
    remedy,
  })];
}

function checkNoColor(probes) {
  return checkColourSwitch(probes, {
    id: 'V-NO-COLOR',
    key: 'noColor',
    title: 'NO_COLOR honoured',
    label: 'NO_COLOR',
    severity: 'serious',
    detail: 'Colour is still emitted with NO_COLOR set. That variable is the one switch users of high-contrast themes and screen readers are told to set, and it is expected to work everywhere.',
    remedy: 'Treat any non-empty NO_COLOR as "disable all colour", per no-color.org. Honour TERM=dumb the same way.',
  });
}

function checkTermDumb(probes) {
  return checkColourSwitch(probes, {
    id: 'V-TERM-DUMB',
    key: 'dumb',
    title: 'TERM=dumb honoured',
    label: 'TERM=dumb',
    severity: 'moderate',
    detail: 'Styling is still emitted under TERM=dumb, which declares a terminal with no such capability. Emacs shell buffers and several screen-reader terminals set it.',
    remedy: 'Check the terminfo capability, or at minimum special-case TERM=dumb alongside NO_COLOR.',
  });
}

function checkColourOnly(probes) {
  const gap = needsTty(probes, 'V-COLOUR-ONLY', 'Colour is not the only signal');
  if (gap) return gap;
  const runs = styledRuns(probes).filter((r) => {
    const fg = r.style.fg;
    return fg && SEMANTIC.has(nearestIndex(fg));
  });
  if (!runs.length) {
    return [U.pass({
      id: 'V-COLOUR-ONLY', dimension: DIM, title: 'Colour is not the only signal',
      detail: 'No red, green or yellow text was used to convey status.',
    })];
  }

  // A textual marker anywhere on the same line means the meaning survives with
  // colour stripped.
  const MARKER = /\b(error|err|warn|warning|fail|failed|failure|ok|pass|passed|success|succeeded|added|removed|deleted|new|skip|skipped|todo|note|info|deprecat|missing|invalid|yes|no)\b|[✓✔✗✘×√!]|^\s*[-+*]\s|\[[!?x+ -]\]|\bE\d{2,}\b/i;

  // A coloured run that is itself a status token — [ok], DOWN, ✗, + — carries
  // its meaning in the text. A coloured run that is a value, like a hostname,
  // does not.
  const isLabel = (text) => {
    const t = text.trim().replace(/^[[(<{]+|[\])>}]+$/g, '').trim();
    if (!t || t.length > 12) return false;
    if (/^[✓✔✗✘×√!+\-*·•]+$/.test(t)) return true;
    if (/^[A-Z][A-Z0-9 _-]{1,11}$/.test(t)) return true;
    return /^(ok|pass(ed)?|fail(ed|ure)?|warn(ing)?|err(or)?|done|skip(ped)?|new|added|removed|deleted|yes|no|todo|info|note|up|down)$/i.test(t);
  };

  const bad = runs.filter((r) => !MARKER.test(r.contextLine) && !isLabel(r.text));

  if (!bad.length) {
    return [U.pass({
      id: 'V-COLOUR-ONLY', dimension: DIM, title: 'Colour is not the only signal',
      detail: 'Every coloured status string also carries a word or symbol that says the same thing.',
    })];
  }

  const uniq = [...new Map(bad.map((r) => [U.excerpt(r.text, 40), r])).values()];
  return [U.fail({
    id: 'V-COLOUR-ONLY',
    dimension: DIM,
    title: 'Colour used as the only signal',
    severity: 'serious',
    detail: `${uniq.length} coloured string(s) carry meaning that disappears entirely when colour is stripped — which is what happens for the roughly 1 in 12 men with a colour vision deficiency, for anyone piping to a file, and for every screen reader.`,
    evidence: uniq.slice(0, 6).map((r) => `${ansi.describe(r.style.fg)}: ${JSON.stringify(U.excerpt(r.text, 50))}`),
    remedy: 'Put the meaning in the text: a word ("error", "added"), a symbol (✓/✗ with an ASCII fallback), or a prefix. Colour should reinforce, never carry.',
  })];
}

function checkAnimation(probes) {
  const gap = needsTty(probes, 'V-ANIMATION', 'In-place redrawing');
  if (gap) return gap;
  const findings = [];
  let worst = null;
  for (const [id, p] of Object.entries(probes)) {
    if (id === '_meta' || !U.ok(p) || !p.tty) continue;
    const { controls } = ansi.parse(p.output);
    const repaints = controls.carriageReturns + controls.eraseLine + controls.eraseDisplay;
    if (repaints < 5) continue;
    const perSecond = repaints / Math.max(0.25, p.durationMs / 1000);
    if (!worst || perSecond > worst.perSecond) {
      worst = { id, repaints, perSecond, durationMs: p.durationMs, controls };
    }
  }

  if (!worst) {
    findings.push(U.pass({
      id: 'V-ANIMATION', dimension: DIM, title: 'No in-place redrawing',
      detail: 'Nothing rewrote a line in place, so output reads once and stays put.',
    }));
    return findings;
  }

  const quiet = U.mentions(U.helpText(probes),
    ['--quiet', '--no-progress', '--no-spinner', '--plain', '--silent', '--progress=']);

  findings.push(U.fail({
    id: 'V-ANIMATION',
    dimension: DIM,
    title: 'Output redrawn in place',
    severity: worst.perSecond > 2 ? 'serious' : 'moderate',
    detail: `${worst.repaints} in-place repaints in ${(worst.durationMs / 1000).toFixed(1)}s (${worst.perSecond.toFixed(1)}/s) during "${worst.id}". A screen reader re-announces a line every time it is rewritten, so a progress spinner becomes a continuous stream of speech that cannot be interrupted or read past.`,
    evidence: [
      `carriage returns: ${worst.controls.carriageReturns}, erase-line: ${worst.controls.eraseLine}, erase-display: ${worst.controls.eraseDisplay}, cursor moves: ${worst.controls.cursorMove}`,
      quiet.length ? `help does mention: ${quiet.join(', ')}` : 'help mentions no flag that turns this off',
    ],
    remedy: quiet.length
      ? 'The escape hatch exists; make sure it is also taken automatically when stdout is not a terminal, and mention it near the progress output.'
      : 'Fall back to one line per event when stdout is not a terminal, and add a --quiet or --no-progress flag. Honouring NO_COLOR here too is a reasonable convention.',
  }));
  return findings;
}

function checkTerminalState(probes) {
  const gap = needsTty(probes, 'V-TERM-STATE', 'Terminal state on exit');
  if (gap) return gap;
  const broken = [];
  for (const [id, p] of Object.entries(probes)) {
    if (id === '_meta' || !U.ok(p) || !p.tty) continue;
    // We killed this one at the timeout, so it never reached its own cleanup.
    // Blaming it for the state we interrupted would be a false accusation.
    if (p.timedOut) continue;
    const { controls } = ansi.parse(p.output);
    if (controls.cursorHide > controls.cursorShow) {
      broken.push(`${id}: cursor hidden ${controls.cursorHide}× but restored ${controls.cursorShow}×`);
    }
    if (controls.altScreenEnter > controls.altScreenLeave) {
      broken.push(`${id}: entered the alternate screen ${controls.altScreenEnter}× but left it ${controls.altScreenLeave}×`);
    }
  }
  if (!broken.length) {
    return [U.pass({
      id: 'V-TERM-STATE', dimension: DIM, title: 'Terminal left in a usable state',
      detail: 'Cursor visibility and screen buffer were restored on exit.',
    })];
  }
  return [U.fail({
    id: 'V-TERM-STATE',
    dimension: DIM,
    title: 'Terminal left in a broken state',
    severity: 'serious',
    detail: 'The CLI exited without restoring what it changed. An invisible cursor or a stuck alternate screen persists into every later command, and the usual fix — typing "reset" blind — is exactly what a new or low-vision user cannot do.',
    evidence: broken.slice(0, 5),
    remedy: 'Restore on every exit path, including SIGINT and unhandled errors, not just the happy one.',
  })];
}

const GLYPH_CLASSES = [
  ['box drawing', (c) => c >= 0x2500 && c <= 0x257f],
  ['block elements', (c) => c >= 0x2580 && c <= 0x259f],
  ['braille (spinner frames)', (c) => c >= 0x2800 && c <= 0x28ff],
  ['emoji', (c) => (c >= 0x1f300 && c <= 0x1faff) || (c >= 0x2600 && c <= 0x27bf)],
  ['arrows and symbols', (c) => (c >= 0x2190 && c <= 0x21ff) || (c >= 0x25a0 && c <= 0x25ff)],
];

function checkGlyphs(probes) {
  const gap = needsTty(probes, 'V-GLYPHS', 'Decorative glyphs');
  if (gap) return gap;
  const counts = new Map();
  const samples = new Map();
  for (const [id, p] of Object.entries(probes)) {
    if (id === '_meta' || !U.ok(p) || !p.tty) continue;
    for (const ch of U.visibleText(p.raw)) {
      const c = ch.codePointAt(0);
      if (c < 0x80) continue;
      for (const [name, test] of GLYPH_CLASSES) {
        if (test(c)) {
          counts.set(name, (counts.get(name) || 0) + 1);
          if (!samples.has(name)) samples.set(name, ch);
        }
      }
    }
  }

  if (!counts.size) {
    return [U.pass({
      id: 'V-GLYPHS', dimension: DIM, title: 'Decorative glyphs',
      detail: 'Output is ASCII, which every terminal and screen reader handles.',
    })];
  }

  // An ASCII fallback under TERM=dumb is the mitigation that matters.
  const dumb = probes.helpDumb;
  let fallback = false;
  if (U.ok(dumb)) {
    const dumbNonAscii = [...U.visibleText(dumb.raw)].filter((ch) => ch.codePointAt(0) > 0x7f).length;
    const ttyNonAscii = U.ok(probes.help)
      ? [...U.visibleText(probes.help.raw)].filter((ch) => ch.codePointAt(0) > 0x7f).length
      : dumbNonAscii;
    fallback = ttyNonAscii > 0 && dumbNonAscii < ttyNonAscii / 2;
  }

  const total = [...counts.values()].reduce((a, b) => a + b, 0);
  const detailParts = [...counts.entries()]
    .sort((a, b) => b[1] - a[1])
    .map(([name, n]) => `${n}× ${name} (${samples.get(name)})`);

  if (fallback) {
    return [U.pass({
      id: 'V-GLYPHS', dimension: DIM, title: 'Decorative glyphs have an ASCII fallback',
      detail: `Uses ${total} non-ASCII glyphs on a capable terminal but falls back under TERM=dumb.`,
      evidence: detailParts.slice(0, 4),
    })];
  }

  const heavy = (counts.get('box drawing') || 0) + (counts.get('block elements') || 0);
  return [U.fail({
    id: 'V-GLYPHS',
    dimension: DIM,
    title: 'Decorative glyphs with no ASCII fallback',
    severity: heavy > 40 ? 'moderate' : 'minor',
    detail: `${total} non-ASCII glyphs with no plainer alternative under TERM=dumb. Screen readers handle these inconsistently: box-drawing characters are often announced cell by cell, turning a table border into a paragraph of speech, and braille spinner frames are read as literal braille.`,
    evidence: detailParts.slice(0, 5),
    remedy: 'Offer an ASCII table/marker style and select it under TERM=dumb, NO_COLOR, or an explicit flag. Never put information only in a glyph.',
  })];
}

function checkPager(probes) {
  const gap = needsTty(probes, 'V-PAGER', 'Paged output');
  if (gap) return gap;

  const paged = Object.entries(probes)
    .filter(([id, p]) => id !== '_meta' && U.isPaging(p))
    .map(([id]) => id);

  if (!paged.length) {
    return [U.pass({
      id: 'V-PAGER', dimension: DIM, title: 'Output is not forced through a pager',
      detail: 'Nothing took over the alternate screen and waited.',
    })];
  }
  return [U.fail({
    id: 'V-PAGER',
    dimension: DIM,
    title: 'Output forced through a pager',
    severity: 'minor',
    detail: `${paged.join(', ')} handed off to a pager, which takes over the alternate screen and waits for keystrokes. Paging is conventional and the piped output is unaffected, but the alternate screen is the part of the terminal screen readers handle worst, and navigating it needs repeated precise keypresses.`,
    evidence: paged.map((id) => `${id}: entered the alternate screen and waited at a pager prompt`),
    remedy: 'Keep the paging, and document the way out — a --no-pager flag, or honouring PAGER=cat. Never page output short enough to fit the screen.',
  })];
}

function checkReflow(probes) {
  const meta = probes._meta;
  const cols = meta.narrowCols;
  const over = [];
  let examined = 0;

  for (const f of meta.families || []) {
    const p = probes[f.narrow];
    if (!U.ok(p)) continue;
    examined++;
    U.visibleLines(p.raw).forEach((l, i) => {
      const w = U.displayWidth(l);
      if (w > cols) over.push({ probe: f.narrow, n: i + 1, w, text: l });
    });
  }

  if (!examined) return needsTty(probes, 'V-REFLOW', 'Reflow to a narrow terminal') || [];
  if (!over.length) {
    return [U.pass({
      id: 'V-REFLOW', dimension: DIM, title: 'Reflows to a narrow terminal',
      detail: `No line exceeded ${cols} columns in a ${cols}-column terminal.`,
    })];
  }

  const worst = over.reduce((a, b) => (b.w > a.w ? b : a));
  return [U.fail({
    id: 'V-REFLOW',
    dimension: DIM,
    title: 'Output overflows a narrow terminal',
    severity: over.length > 10 ? 'moderate' : 'minor',
    detail: `${over.length} line(s) are wider than the ${cols}-column terminal they were printed into, the widest at ${worst.w} columns. A magnifier user at 4× has roughly this much width, and hard wrapping mid-word plus aligned columns that no longer align is what they get.`,
    evidence: over.slice(0, 4).map((l) => `${l.probe} line ${l.n} (${l.w} cols): ${U.excerpt(l.text, 55)}`),
    remedy: 'Wrap on word boundaries to the real terminal width, and collapse multi-column layouts below about 60 columns.',
  })];
}

function checkWidthAwareness(probes) {
  const meta = probes._meta;
  const maxW = (p) => Math.max(0, ...U.visibleLines(p.raw).map(U.displayWidth));
  const rows = [];

  for (const f of meta.families || []) {
    const narrow = probes[f.narrow];
    const wide = probes[f.wide];
    if (!U.ok(narrow) || !U.ok(wide)) continue;
    rows.push({ name: f.name, nw: maxW(narrow), ww: maxW(wide) });
  }

  if (!rows.length) return needsTty(probes, 'V-WIDTH', 'Terminal width awareness') || [];

  const fits = rows.filter((r) => r.nw <= meta.narrowCols);
  const frozen = rows.filter((r) => r.nw > meta.narrowCols && r.nw === r.ww && r.ww > meta.narrowCols);

  if (fits.length === rows.length) {
    return [U.pass({
      id: 'V-WIDTH', dimension: DIM, title: 'Respects the terminal width',
      detail: `Output narrowed to fit a ${meta.narrowCols}-column terminal.`,
    })];
  }
  if (frozen.length) {
    return [U.fail({
      id: 'V-WIDTH',
      dimension: DIM,
      title: 'Terminal width ignored',
      severity: 'moderate',
      detail: `Output is the same width whether the terminal is ${meta.narrowCols} or ${meta.wideCols} columns. The layout is hard-coded, so it can never fit a resized or magnified window.`,
      evidence: frozen.slice(0, 3).map((r) => `${r.name}: ${r.nw} columns at both ${meta.narrowCols} and ${meta.wideCols} columns of terminal`),
      remedy: 'Read the width from the terminal (ioctl, or $COLUMNS as a fallback) and lay out against it, clamping to a sane minimum.',
    })];
  }
  return [U.pass({
    id: 'V-WIDTH', dimension: DIM, title: 'Responds to the terminal width',
    detail: 'Output width changed with the terminal, though some lines still overflow.',
  })];
}

function checks(probes) {
  return [
    ...checkPipeColour(probes),
    ...checkNoColor(probes),
    ...checkTermDumb(probes),
    ...checkContrast(probes),
    ...checkDim(probes),
    ...checkColourOnly(probes),
    ...checkAnimation(probes),
    ...checkTerminalState(probes),
    ...checkGlyphs(probes),
    ...checkPager(probes),
    ...checkReflow(probes),
    ...checkWidthAwareness(probes),
  ];
}

module.exports = { checks, nearestIndex, applyDim, defaultFgFor };
