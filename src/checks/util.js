'use strict';

const ansi = require('../ansi');

const SEVERITIES = ['critical', 'serious', 'moderate', 'minor'];

function finding(o) {
  return {
    status: 'fail',
    severity: 'moderate',
    evidence: [],
    remedy: '',
    ...o,
  };
}

const fail = (o) => finding({ ...o, status: 'fail' });
const pass = (o) => finding({ ...o, status: 'pass', severity: null });
const skip = (o) => finding({ ...o, status: 'skip', severity: null });

/** Did this probe actually produce a usable run? */
function ok(p) {
  return p && !p.skipped && !p.error;
}

/**
 * Render a line the way a terminal would: a carriage return sends the cursor
 * back to column zero and what follows overwrites what was there. Without this
 * a spinner looks like a hundred-character line rather than the eight the user
 * sees.
 */
function renderLine(line) {
  let buf = '';
  let col = 0;
  for (const ch of line) {
    if (ch === '\r') { col = 0; continue; }
    if (col < buf.length) buf = buf.slice(0, col) + ch + buf.slice(col + 1);
    else buf += ch;
    col++;
  }
  return buf;
}

/** The visible lines of a run, escapes removed and overwrites applied. */
function visibleLines(raw) {
  return ansi.strip(raw).split('\n').map(renderLine);
}

/** Plain visible text, escapes removed and overwrites applied. */
function visibleText(raw) {
  return visibleLines(raw).join('\n');
}

// The scattered wide characters in the symbol blocks — ✅ ❌ ⚡ ✨ and friends —
// which terminals draw two cells wide despite sitting below U+1F300.
const WIDE_SYMBOLS = [
  [0x231a, 0x231b], [0x23e9, 0x23ec], [0x23f0, 0x23f0], [0x23f3, 0x23f3],
  [0x25fd, 0x25fe], [0x2614, 0x2615], [0x2648, 0x2653], [0x267f, 0x267f],
  [0x2693, 0x2693], [0x26a1, 0x26a1], [0x26aa, 0x26ab], [0x26bd, 0x26be],
  [0x26c4, 0x26c5], [0x26ce, 0x26ce], [0x26d4, 0x26d4], [0x26ea, 0x26ea],
  [0x26f2, 0x26f3], [0x26f5, 0x26f5], [0x26fa, 0x26fa], [0x26fd, 0x26fd],
  [0x2705, 0x2705], [0x270a, 0x270b], [0x2728, 0x2728], [0x274c, 0x274c],
  [0x274e, 0x274e], [0x2753, 0x2755], [0x2757, 0x2757], [0x2795, 0x2797],
  [0x27b0, 0x27b0], [0x27bf, 0x27bf], [0x2b1b, 0x2b1c], [0x2b50, 0x2b50],
  [0x2b55, 0x2b55],
];

// Rough terminal column width: emoji and East Asian wide characters occupy two
// cells, combining marks none.
function displayWidth(str) {
  let w = 0;
  for (const ch of str) {
    const c = ch.codePointAt(0);
    if (c === 0x200d || (c >= 0xfe00 && c <= 0xfe0f)) continue;
    if (c >= 0x0300 && c <= 0x036f) continue;
    if (
      (c >= 0x1100 && c <= 0x115f) || (c >= 0x2e80 && c <= 0xa4cf)
      || (c >= 0xac00 && c <= 0xd7a3) || (c >= 0xf900 && c <= 0xfaff)
      || (c >= 0xfe30 && c <= 0xfe6f) || (c >= 0xff00 && c <= 0xff60)
      || (c >= 0xffe0 && c <= 0xffe6) || (c >= 0x1f300 && c <= 0x1faff)
      || (c >= 0x1f000 && c <= 0x1f0ff) || WIDE_SYMBOLS.some(([a, b]) => c >= a && c <= b)
    ) { w += 2; continue; }
    w += 1;
  }
  return w;
}

/** A short, safe excerpt of a line for the report. */
function excerpt(s, max = 72) {
  const one = String(s).replace(/\s+/g, ' ').trim();
  return one.length > max ? `${one.slice(0, max - 1)}…` : one;
}

/**
 * Is this run sitting in a pager rather than hung?
 *
 * less and more take over the alternate screen and wait at a prompt, which from
 * the outside looks exactly like a CLI that stopped and never came back. The
 * difference matters: paging is conventional behaviour, hanging is a defect.
 */
function isPaging(probe) {
  if (!ok(probe) || !probe.timedOut) return false;
  const { controls } = ansi.parse(probe.output);
  if (controls.altScreenEnter <= controls.altScreenLeave) return false;
  const tail = visibleText(probe.raw).trimEnd().slice(-40);
  return /(?::|\(END\)|--More--|lines \d+-\d+|byte \d+)$/.test(tail) || controls.altScreenEnter > 0;
}

/** Everything a CLI's help text says it accepts, lowercased for matching. */
function helpText(probes) {
  const sources = [probes.help, probes.helpPipe, probes.helpDumb];
  for (const p of sources) {
    if (ok(p) && visibleText(p.raw).trim().length > 20) return visibleText(p.raw);
  }
  return '';
}

/** Does the help mention any of these flags or words? */
function mentions(text, patterns) {
  const hay = text.toLowerCase();
  return patterns.filter((p) => hay.includes(p.toLowerCase()));
}

module.exports = {
  SEVERITIES, finding, fail, pass, skip, ok,
  renderLine, visibleLines, visibleText, displayWidth, excerpt,
  helpText, mentions, isPaging,
};
