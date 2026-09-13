'use strict';

// Terminal escape-sequence parsing, colour resolution and WCAG contrast.
// Everything here works on the raw bytes a CLI actually wrote, because that is
// the only thing a screen reader or a low-vision user ever gets.

const ESC = '\x1b';
const BEL = '\x07';
const ST = '\x9c';

// xterm's default palette for the 16 named colours. Terminals re-theme these,
// but a CLI that fails contrast against the defaults fails for most people.
const BASE16 = [
  [0, 0, 0], [205, 0, 0], [0, 205, 0], [205, 205, 0],
  [0, 0, 238], [205, 0, 205], [0, 205, 205], [229, 229, 229],
  [127, 127, 127], [255, 0, 0], [0, 255, 0], [255, 255, 0],
  [92, 92, 255], [255, 0, 255], [0, 255, 255], [255, 255, 255],
];

const NAMES = ['black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white',
  'bright black', 'bright red', 'bright green', 'bright yellow',
  'bright blue', 'bright magenta', 'bright cyan', 'bright white'];

const CUBE = [0, 95, 135, 175, 215, 255];

function xterm256(n) {
  if (n < 16) return BASE16[n];
  if (n < 232) {
    const i = n - 16;
    return [CUBE[Math.floor(i / 36) % 6], CUBE[Math.floor(i / 6) % 6], CUBE[i % 6]];
  }
  const v = 8 + (n - 232) * 10;
  return [v, v, v];
}

function luminance([r, g, b]) {
  const f = (c) => {
    const s = c / 255;
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
}

function contrast(a, b) {
  const [x, y] = [luminance(a), luminance(b)].sort((m, n) => n - m);
  return (x + 0.05) / (y + 0.05);
}

function hex(rgb) {
  return '#' + rgb.map((c) => Math.max(0, Math.min(255, Math.round(c))).toString(16).padStart(2, '0')).join('');
}

/** Nearest named colour, for describing an arbitrary RGB in a report. */
function describe(rgb) {
  let best = 0;
  let bestD = Infinity;
  BASE16.forEach((c, i) => {
    const d = (c[0] - rgb[0]) ** 2 + (c[1] - rgb[1]) ** 2 + (c[2] - rgb[2]) ** 2;
    if (d < bestD) { bestD = d; best = i; }
  });
  return bestD === 0 ? NAMES[best] : `${NAMES[best]}-ish ${hex(rgb)}`;
}

// The two backgrounds worth testing against: a typical dark terminal and a
// typical light one. A colour that only works on one of them is a real problem,
// because the CLI cannot know which the user has.
const DARK_BG = [30, 30, 30];
const LIGHT_BG = [255, 255, 255];

function blankStyle() {
  return {
    fg: null, bg: null, bold: false, dim: false, italic: false,
    underline: false, reverse: false, fgName: null, bgName: null,
  };
}

// Apply one SGR (Select Graphic Rendition) sequence to a style.
function applySgr(style, params) {
  const p = params.length ? params : [0];
  for (let i = 0; i < p.length; i++) {
    const n = p[i];
    if (n === 0) Object.assign(style, blankStyle());
    else if (n === 1) style.bold = true;
    else if (n === 2) style.dim = true;
    else if (n === 3) style.italic = true;
    else if (n === 4) style.underline = true;
    else if (n === 7) style.reverse = true;
    else if (n === 22) { style.bold = false; style.dim = false; }
    else if (n === 23) style.italic = false;
    else if (n === 24) style.underline = false;
    else if (n === 27) style.reverse = false;
    else if (n >= 30 && n <= 37) { style.fg = BASE16[n - 30]; style.fgName = String(n); }
    else if (n === 39) { style.fg = null; style.fgName = null; }
    else if (n >= 40 && n <= 47) { style.bg = BASE16[n - 40]; style.bgName = String(n); }
    else if (n === 49) { style.bg = null; style.bgName = null; }
    else if (n >= 90 && n <= 97) { style.fg = BASE16[n - 90 + 8]; style.fgName = String(n); }
    else if (n >= 100 && n <= 107) { style.bg = BASE16[n - 100 + 8]; style.bgName = String(n); }
    else if (n === 38 || n === 48) {
      const target = n === 38 ? 'fg' : 'bg';
      const nameKey = n === 38 ? 'fgName' : 'bgName';
      if (p[i + 1] === 5) {
        style[target] = xterm256(p[i + 2] || 0);
        style[nameKey] = `${n};5;${p[i + 2]}`;
        i += 2;
      } else if (p[i + 1] === 2) {
        style[target] = [p[i + 2] || 0, p[i + 3] || 0, p[i + 4] || 0];
        style[nameKey] = `${n};2;${p[i + 2]};${p[i + 3]};${p[i + 4]}`;
        i += 4;
      }
    }
  }
}

// Many terminals render bold + a basic colour as the bright variant, so the
// colour a CLI gets is often not the one it named.
function effectiveFg(style) {
  if (!style.fg) return null;
  if (style.bold && style.fgName && /^3[0-7]$/.test(style.fgName)) {
    return BASE16[Number(style.fgName) - 30 + 8];
  }
  return style.fg;
}

/**
 * Walk a raw output stream, returning the visible text, the styled runs within
 * it, and a tally of the control sequences that affect assistive tech.
 */
function parse(raw) {
  const style = blankStyle();
  const runs = [];
  let plain = '';
  let current = null;
  const controls = {
    carriageReturns: 0, eraseLine: 0, eraseDisplay: 0, cursorMove: 0,
    cursorHide: 0, cursorShow: 0, altScreenEnter: 0, altScreenLeave: 0,
    bell: 0, osc: 0, sgr: 0,
  };

  const flush = () => {
    if (current && current.text.length) runs.push(current);
    current = null;
  };

  for (let i = 0; i < raw.length; i++) {
    const ch = raw[i];

    if (ch === ESC) {
      const next = raw[i + 1];
      if (next === '[') {
        // CSI: ESC [ params intermediates final
        let j = i + 2;
        let body = '';
        while (j < raw.length && !/[@-~]/.test(raw[j])) { body += raw[j]; j++; }
        const final = raw[j];
        const isPrivate = /^[?<>=]/.test(body);
        const nums = body.replace(/^[?<>=]/, '').split(';')
          .map((s) => (s === '' ? 0 : Number(s)))
          .filter((n) => !Number.isNaN(n));

        if (final === 'm' && !isPrivate) {
          flush();
          applySgr(style, nums);
          controls.sgr++;
        } else if (final === 'K') controls.eraseLine++;
        else if (final === 'J') controls.eraseDisplay++;
        else if (final && 'ABCDEFGHfd'.includes(final)) controls.cursorMove++;
        else if (isPrivate && (final === 'h' || final === 'l')) {
          const mode = nums[0];
          if (mode === 25) { if (final === 'l') controls.cursorHide++; else controls.cursorShow++; }
          if (mode === 1049 || mode === 47 || mode === 1047) {
            if (final === 'h') controls.altScreenEnter++; else controls.altScreenLeave++;
          }
        }
        i = j === undefined ? raw.length : j;
        continue;
      }
      if (next === ']') {
        // OSC: ends at BEL or ST
        let j = i + 2;
        while (j < raw.length && raw[j] !== BEL && raw[j] !== ST
               && !(raw[j] === ESC && raw[j + 1] === '\\')) j++;
        controls.osc++;
        i = raw[j] === ESC ? j + 1 : j;
        continue;
      }
      i++; // some other two-byte escape
      continue;
    }

    if (ch === '\r') { controls.carriageReturns++; plain += ch; continue; }
    if (ch === BEL) { controls.bell++; continue; }

    plain += ch;
    if (ch === '\n') { flush(); continue; }

    if (!current) {
      current = {
        text: '',
        style: { ...style, fg: effectiveFg(style) },
        line: plain.split('\n').length - 1,
      };
    }
    current.text += ch;
  }
  flush();

  return { plain, runs, controls };
}

/** Strip every escape sequence, leaving what a plain-text reader would get. */
function strip(raw) {
  return parse(raw).plain;
}

// Which SGR parameters actually change a colour. Bold, underline and italic are
// emphasis: they survive a monochrome terminal, they carry meaning without it,
// and NO_COLOR is not asking anyone to give them up. Faint and reverse do change
// what colour lands on the screen, so they count.
function isColourParam(n) {
  return (n >= 30 && n <= 49) || (n >= 90 && n <= 97) || (n >= 100 && n <= 107)
    || n === 2 || n === 7;
}

/**
 * True if the stream sets a colour, as opposed to merely emphasising text.
 *
 * The distinction matters more than it looks. A pager renders man-page style
 * help with bold and underline, and those bytes arrive in our capture mixed in
 * with the CLI's own; counting them as colour means accusing a perfectly
 * well-behaved tool of ignoring NO_COLOR.
 */
function hasColour(raw) {
  const matches = raw.match(/\x1b\[[0-9;]*m/g);
  if (!matches) return false;
  return matches.some((seq) => {
    const body = seq.slice(2, -1);
    const params = body === '' ? [0] : body.split(';').map(Number);
    return params.some((n) => !Number.isNaN(n) && isColourParam(n));
  });
}

module.exports = {
  parse, strip, hasColour, isColourParam, contrast, luminance, hex, describe,
  effectiveFg, xterm256, BASE16, NAMES, DARK_BG, LIGHT_BG, ESC,
};
