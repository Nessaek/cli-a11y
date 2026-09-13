#!/usr/bin/env node
'use strict';

// The same tool, built the other way round. The test suite asserts that
// cli-a11y passes each check this one is designed to satisfy.

const args = process.argv.slice(2);

const colourOk = Boolean(process.stdout.isTTY)
  && !process.env.NO_COLOR
  && process.env.TERM !== 'dumb';

// Bright variants, which clear WCAG AA against both a dark and a light
// terminal. Nothing faint, and colour never carries meaning on its own.
const paint = (code) => (s) => (colourOk ? `\x1b[${code}m${s}\x1b[0m` : String(s));
const red = paint(91);
const green = paint(92);

const width = Math.max(30, Number(process.env.COLUMNS) || process.stdout.columns || 80);

function wrap(text, indent = '') {
  const out = [];
  let line = indent;
  for (const word of text.split(/\s+/).filter(Boolean)) {
    if (line.length > indent.length && line.length + 1 + word.length > width) {
      out.push(line);
      line = indent + word;
    } else line = line.length > indent.length ? `${line} ${word}` : indent + word;
  }
  out.push(line);
  return out.join('\n');
}

// Every line goes through the wrapper, including the option list, so the help
// fits whatever terminal it is printed into.
function option(flag, text) {
  const gutter = 20;
  const head = `  ${flag}`.padEnd(gutter);
  // wrap() indents every line; drop the indent from the first so the flag sits
  // in front of it and continuation lines line up under the description.
  return head + wrap(text, ' '.repeat(gutter)).slice(gutter);
}

function help() {
  process.stdout.write([
    wrap('Usage: deploytool [options] [environment]'),
    '',
    wrap('Deploys the current project to an environment. With no environment, '
      + 'reports what is running where.'),
    '',
    'Options:',
    option('--target <name>', 'Environment to deploy to.'),
    option('--yes', 'Answer every prompt with yes. Use this in CI.'),
    option('--json', 'Print machine-readable output instead of a table.'),
    option('--quiet', 'Suppress progress output.'),
    option('--version', 'Print the version.'),
    option('--help', 'Print this help.'),
    '',
    'Examples:',
    wrap('$ deploytool --target staging', '  '),
    wrap('$ deploytool --target production --yes', '  '),
    '',
  ].join('\n'));
  process.exit(0);
}

function status() {
  // The word says it; the colour only reinforces it.
  const rows = [
    ['ok', 'api-gateway', '2 replicas'],
    ['DOWN', 'worker-queue', '0 replicas'],
    ['ok', 'scheduler', '1 replica'],
  ];
  if (args.includes('--json')) {
    process.stdout.write(JSON.stringify(rows.map(([s, n, d]) => ({ status: s, name: n, detail: d })), null, 2) + '\n');
    return;
  }
  for (const [state, name, detail] of rows) {
    const mark = state === 'ok' ? green('[ok]  ') : red('[DOWN]');
    process.stdout.write(wrap(`${mark} ${name} — ${detail}`) + '\n');
  }
}

function badFlag(flag) {
  process.stderr.write(`deploytool: unrecognised option "${flag}".\n`);
  process.stderr.write('Did you mean --target? Run "deploytool --help" for the full list.\n');
  process.exit(2);
}

if (args.includes('--help') || args.includes('-h')) help();
else if (args.includes('--version')) { process.stdout.write('deploytool 2.4.1\n'); process.exit(0); }
else {
  const known = ['--target', '--yes', '--json', '--quiet', '--help', '--version'];
  const unknown = args.find((a) => a.startsWith('-') && !known.includes(a));
  if (unknown) badFlag(unknown);
  status();
  process.exit(0);
}
