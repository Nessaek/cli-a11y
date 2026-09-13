#!/usr/bin/env node
'use strict';

// A deliberately inaccessible CLI. Every fault here is one seen in real tools.
// The test suite asserts that cli-a11y finds each of them.

const args = process.argv.slice(2);

// Colour unconditionally: no isatty check, no NO_COLOR, no TERM=dumb.
const red = (s) => `\x1b[31m${s}\x1b[0m`;
const green = (s) => `\x1b[32m${s}\x1b[0m`;
const faint = (s) => `\x1b[2m${s}\x1b[0m`;

function help() {
  // Wrong stream, wrong exit code, no usage line, no examples, hard-coded width.
  process.stderr.write([
    faint('deploytool — the deployment tool'),
    '',
    '  ' + '─'.repeat(96),
    '  │ ' + faint('Configure the deployment by passing the appropriate configuration flags, noting that some') + ' │',
    '  │ ' + faint('of these are mutually exclusive and that the precedence rules are somewhat involved and') + '  │',
    '  │ ' + faint('are described at length in the documentation which you should probably read first.') + '      │',
    '  ' + '─'.repeat(96),
    '',
    '  --target      the target',
    '  --strategy    the strategy',
    '  --config      the config',
    '',
  ].join('\n') + '\n');
  process.exit(1);
}

function statusTable() {
  // Colour as the only signal: nothing but the colour says which passed.
  process.stdout.write('  ' + '━'.repeat(40) + '\n');
  process.stdout.write('  ' + green('api-gateway') + '     2 replicas\n');
  process.stdout.write('  ' + red('worker-queue') + '    0 replicas\n');
  process.stdout.write('  ' + green('scheduler') + '       1 replica\n');
  process.stdout.write('  ' + '━'.repeat(40) + '\n');
}

async function bare() {
  process.stdout.write('\x1b[?25l'); // hide the cursor and never restore it
  const frames = ['⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧'];
  const started = Date.now();
  let i = 0;
  while (Date.now() - started < 1200) {
    process.stdout.write(`\r\x1b[2K${frames[i++ % frames.length]} ${faint('resolving manifests…')}`);
    await new Promise((r) => setTimeout(r, 70));
  }
  process.stdout.write('\r\x1b[2K');
  statusTable();
  process.stdout.write('\nUse arrow keys to choose an environment, then press <space>.\n');
  process.stdout.write('Continuing automatically in 10 seconds.\n');
  process.stdout.write('Are you sure you want to continue? [y/N] ');
  process.stdin.resume(); // wait forever, with no --yes to be found anywhere
}

if (args.includes('--help') || args.includes('-h')) help();
else if (args.includes('--version')) process.exit(0); // prints nothing
else if (args.length && args[0].startsWith('-')) {
  // Unknown flag: wrong stream, useless message, success exit code.
  process.stdout.write('Something went wrong.\n');
  process.exit(0);
} else bare();
