#!/usr/bin/env node
'use strict';

// Every rule is pinned to a fixture that provokes it and a fixture that does
// not. A rule that cannot tell the two apart is worse than no rule, because it
// spends the reader's attention on nothing.

const path = require('path');
const { spawnSync } = require('child_process');
const { audit } = require('../src/index');
const ansi = require('../src/ansi');
const U = require('../src/checks/util');
const report = require('../src/report');

const FIXTURES = path.join(__dirname, 'fixtures');
const BIN = path.join(__dirname, '..', 'bin', 'cli-a11y');

let passed = 0;
const failures = [];

function check(name, fn) {
  try {
    const problem = fn();
    if (problem) failures.push(`${name}: ${problem}`);
    else passed++;
  } catch (err) {
    failures.push(`${name}: threw ${err && err.message}`);
  }
}

const eq = (actual, expected, what) => (actual === expected
  ? null : `${what}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);

const near = (actual, expected, tol, what) => (Math.abs(actual - expected) <= tol
  ? null : `${what}: expected ${expected}±${tol}, got ${actual}`);

// ---------------------------------------------------------------- unit: ansi

check('ansi: strips escapes', () => eq(
  ansi.strip('\x1b[31mred\x1b[0m plain'), 'red plain', 'stripped text',
));

check('ansi: resolves bold + basic colour to the bright variant', () => {
  const { runs } = ansi.parse('\x1b[1;33mwarn\x1b[0m');
  return eq(ansi.hex(runs[0].style.fg), '#ffff00', 'effective foreground');
});

check('ansi: reads 256-colour and truecolour', () => {
  const { runs } = ansi.parse('\x1b[38;5;196ma\x1b[0m\x1b[38;2;18;52;86mb\x1b[0m');
  return eq(ansi.hex(runs[0].style.fg), '#ff0000', '256-colour')
    || eq(ansi.hex(runs[1].style.fg), '#123456', 'truecolour');
});

check('ansi: counts the controls that matter to a screen reader', () => {
  const { controls } = ansi.parse('\rspin\x1b[2K\x1b[?25l\x1b[?1049h\x1b[3A');
  return eq(controls.carriageReturns, 1, 'carriage returns')
    || eq(controls.eraseLine, 1, 'erase line')
    || eq(controls.cursorHide, 1, 'cursor hide')
    || eq(controls.altScreenEnter, 1, 'alt screen')
    || eq(controls.cursorMove, 1, 'cursor moves');
});

check('ansi: does not mistake a lone reset for colour', () => eq(
  ansi.hasColour('\x1b[0mplain'), false, 'hasColour',
));

check('ansi: WCAG contrast matches the reference values', () => eq(
  ansi.contrast([255, 255, 255], [0, 0, 0]).toFixed(0), '21', 'white on black',
) || near(ansi.contrast([0, 0, 0], [0, 0, 0]), 1, 0.001, 'black on black'));

// ---------------------------------------------------------------- unit: util

check('util: a carriage return overwrites rather than appends', () => eq(
  U.renderLine('loading 90%\rdone'), 'doneing 90%', 'rendered line',
));

check('util: emoji and wide characters take two columns', () => eq(
  U.displayWidth('ab✅'), 4, 'display width',
) || eq(U.displayWidth('日本'), 4, 'wide characters'));

// -------------------------------------------------------------- unit: report

check('report: score falls with severity', () => eq(
  report.score([
    { status: 'fail', severity: 'serious' },
    { status: 'fail', severity: 'minor' },
    { status: 'pass' },
  ]), 89, 'score',
));

check('report: --fail-on respects the threshold', () => {
  const f = [{ status: 'fail', severity: 'moderate' }];
  return eq(report.shouldFail(f, 'serious'), false, 'moderate against serious')
    || eq(report.shouldFail(f, 'moderate'), true, 'moderate against moderate')
    || eq(report.shouldFail(f, 'none'), false, 'moderate against none');
});

check('report: its own output honours NO_COLOR', () => {
  const result = {
    target: { cmd: 'x', args: [] },
    meta: { pty: true },
    findings: [{
      id: 'T', dimension: 'vision', status: 'fail', severity: 'serious',
      title: 'T', detail: 'd', evidence: [], remedy: 'r',
    }],
  };
  const text = report.render(result, { color: false });
  return ansi.hasColour(text) ? 'rendered escapes with colour disabled' : null;
});

// ------------------------------------------------------- integration: rules

// Each rule must fire on bad-cli.js and clear on good-cli.js.
const MUST_FAIL_ON_BAD = [
  'V-PIPE-COLOUR', 'V-NO-COLOR', 'V-TERM-DUMB', 'V-CONTRAST', 'V-DIM',
  'V-COLOUR-ONLY', 'V-ANIMATION', 'V-TERM-STATE', 'V-GLYPHS', 'V-WIDTH', 'V-REFLOW',
  'M-INTERACTIVE-HANG', 'M-PIPE-PROMPT', 'M-BATCH-FLAG', 'M-ARROW-MENU',
  'M-TIMED-PROMPT', 'M-MACHINE-OUTPUT',
  'C-HELP-EXIT', 'C-HELP-STREAM', 'C-VERSION', 'C-ERROR-EXIT', 'C-ERROR-STREAM',
  'C-ERROR-ACTIONABLE', 'C-HELP-STRUCTURE', 'C-HELP-EXAMPLES', 'C-READABILITY',
];

// V-CONTRAST is absent here on purpose: the ANSI bright red that good-cli.js
// uses for its failure marker is 4.0:1 on a light terminal, so it cannot pass.
// That is a true finding about the palette, not a defect in the fixture.
const MUST_PASS_ON_GOOD = [
  'V-PIPE-COLOUR', 'V-NO-COLOR', 'V-TERM-DUMB', 'V-DIM', 'V-COLOUR-ONLY',
  'V-ANIMATION', 'V-TERM-STATE', 'V-GLYPHS', 'V-WIDTH', 'V-REFLOW',
  'M-INTERACTIVE-HANG', 'M-PIPE-PROMPT', 'M-BATCH-FLAG', 'M-ARROW-MENU',
  'M-TIMED-PROMPT', 'M-MACHINE-OUTPUT',
  'C-HELP-EXISTS', 'C-HELP-EXIT', 'C-HELP-STREAM', 'C-VERSION', 'C-ERROR-EXIT',
  'C-ERROR-STREAM', 'C-ERROR-ACTIONABLE', 'C-HELP-STRUCTURE', 'C-HELP-EXAMPLES',
  'C-READABILITY',
];

async function integration() {
  const opts = { timeout: 2000 };
  const bad = await audit({ cmd: 'node', args: [path.join(FIXTURES, 'bad-cli.js')] }, opts);
  const good = await audit({ cmd: 'node', args: [path.join(FIXTURES, 'good-cli.js')] }, opts);

  const status = (result, id) => {
    const f = result.findings.find((x) => x.id === id);
    return f ? f.status : 'missing';
  };

  for (const id of MUST_FAIL_ON_BAD) {
    check(`bad-cli fails ${id}`, () => eq(status(bad, id), 'fail', 'status'));
  }
  for (const id of MUST_PASS_ON_GOOD) {
    check(`good-cli passes ${id}`, () => eq(status(good, id), 'pass', 'status'));
  }

  check('bad-cli scores near zero', () => (report.score(bad.findings) <= 20
    ? null : `score was ${report.score(bad.findings)}`));
  check('good-cli scores well', () => (report.score(good.findings) >= 90
    ? null : `score was ${report.score(good.findings)}`));

  check('every finding carries a remedy', () => {
    const missing = [...bad.findings, ...good.findings]
      .filter((f) => f.status === 'fail' && !f.remedy)
      .map((f) => f.id);
    return missing.length ? `no remedy on ${missing.join(', ')}` : null;
  });

  check('no probe leaves a temp file behind', () => {
    const left = spawnSync('/bin/sh', ['-c', 'ls /tmp/cli-a11y-* 2>/dev/null | wc -l']);
    return Number(String(left.stdout).trim()) === 0 ? null : 'temp files left in /tmp';
  });

  // ------------------------------------------------------- integration: bin
  const run = (args) => spawnSync(BIN, args, { encoding: 'utf8', timeout: 60000 });

  check('bin exits 1 on findings', () => {
    const r = run(['--no-color', '--timeout', '2000', '--', 'node', path.join(FIXTURES, 'bad-cli.js')]);
    return eq(r.status, 1, 'exit code');
  });

  check('bin exits 0 when nothing meets the threshold', () => {
    const r = run(['--no-color', '--fail-on', 'none', '--timeout', '2000', '--', 'node', path.join(FIXTURES, 'bad-cli.js')]);
    return eq(r.status, 0, 'exit code');
  });

  check('bin emits valid JSON', () => {
    const r = run(['--json', '--timeout', '2000', '--', 'node', path.join(FIXTURES, 'good-cli.js')]);
    let parsed;
    try { parsed = JSON.parse(r.stdout); } catch (e) { return `stdout was not JSON: ${e.message}`; }
    return parsed.findings && parsed.findings.length > 20
      ? null : `only ${parsed.findings && parsed.findings.length} findings`;
  });

  check('bin exits 2 on a command it cannot run', () => {
    const r = run(['--timeout', '2000', '--', 'definitely-not-a-real-binary-xyz']);
    return eq(r.status, 2, 'exit code');
  });

  check('bin rejects an unknown option', () => {
    const r = run(['--nope', '--', 'node', '-e', '1']);
    return eq(r.status, 2, 'exit code');
  });

  // Auditing the bare binary, not "cli-a11y --help": the auditor appends its
  // own flags, and a target whose first argument already short-circuits to help
  // answers every probe with the same help text.
  check('bin passes its own audit', () => {
    const r = run(['--no-color', '--fail-on', 'serious', '--timeout', '4000', '--', BIN]);
    return r.status === 0 ? null
      : `cli-a11y failed its own checks (exit ${r.status}):\n${r.stdout}`;
  });
}

integration().then(() => {
  process.stdout.write(`\n${passed} passed, ${failures.length} failed\n`);
  if (failures.length) {
    process.stdout.write(`\n${failures.map((f) => `  FAIL ${f}`).join('\n')}\n`);
    process.exit(1);
  }
  process.exit(0);
}).catch((err) => {
  process.stderr.write(`test harness error: ${err.stack}\n`);
  process.exit(1);
});
