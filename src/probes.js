'use strict';

// The environment matrix. Each probe is one execution of the target under
// conditions a real user might have: a terminal, a pipe, NO_COLOR set, a
// screen-reader-friendly TERM, a narrow window.
//
// Probes are grouped into families — the same invocation under several
// conditions — because most of the interesting questions are comparisons.
// "Does it honour NO_COLOR?" only means something against the same command run
// without it, and colour usually lives in the tool's real output rather than in
// its help text, so every family is exercised, not just --help.
//
// Checks never spawn anything themselves. They read these results, so the
// target is run a fixed number of times no matter how many rules exist.

const { run, ptyAvailable } = require('./run');

const NARROW_COLS = 40;
const WIDE_COLS = 100;

/**
 * @param {object} target  {cmd, args} for the CLI under test
 * @param {object} opts    {timeout, cwd, helpFlag, versionFlag, extra: [{label, args}]}
 */
async function collect(target, opts = {}) {
  const {
    timeout = 10000,
    cwd = process.cwd(),
    helpFlag = '--help',
    versionFlag = '--version',
    extra = [],
    onProgress = () => {},
  } = opts;

  const pty = ptyAvailable();
  const base = { timeout, cwd };
  const A = (...more) => [...target.args, ...more];

  // A flag nothing sane implements, used to provoke the error path.
  const BAD_FLAG = '--cli-a11y-nonexistent-flag';

  // A bare run may never terminate by design, so it gets a shorter leash.
  const bareTimeout = Math.min(timeout, 6000);

  const plan = [];
  const families = [];

  /**
   * Queue one invocation under every condition worth comparing.
   *
   * Where the family is meant to catch a CLI that waits for input, only the
   * plain terminal and piped runs hold stdin open; the rest get an immediate
   * EOF so a genuinely interactive tool costs two timeouts rather than six.
   */
  function family(name, args, extraOpts = {}) {
    const ids = { name, args };
    const holds = extraOpts.stdin === 'hold';
    const add = (suffix, o) => {
      const id = suffix ? name + suffix : name;
      const stdin = holds && (suffix === '' || suffix === 'Pipe') ? 'hold' : 'close';
      plan.push([id, args, { ...extraOpts, stdin, ...o }]);
      return id;
    };
    ids.tty = add('', { tty: true, cols: 80 });
    ids.pipe = add('Pipe', { tty: false });
    ids.noColor = add('NoColor', { tty: true, env: { NO_COLOR: '1' } });
    ids.dumb = add('Dumb', { tty: true, env: { TERM: 'dumb' } });
    ids.narrow = add('Narrow', { tty: true, cols: NARROW_COLS });
    ids.wide = add('Wide', { tty: true, cols: WIDE_COLS });
    families.push(ids);
    return ids;
  }

  family('help', A(helpFlag));
  family('bare', A(), { stdin: 'hold', timeout: bareTimeout });
  extra.forEach((e, i) => family(`extra${i}`, [...target.args, ...e.args]));

  // Single runs: nothing to compare them against.
  plan.push(['version', A(versionFlag), { tty: true }]);
  plan.push(['badFlag', A(BAD_FLAG), { tty: false }]);

  const results = {};
  let done = 0;
  for (const [id, args, o] of plan) {
    if (o.tty && !pty) {
      results[id] = { skipped: 'no pty available on this machine', id };
      continue;
    }
    onProgress(id, ++done, plan.length);
    const r = await run(target.cmd, args, { ...base, ...o });
    results[id] = { ...r, id };
  }

  results._meta = {
    pty,
    badFlag: BAD_FLAG,
    helpFlag,
    versionFlag,
    narrowCols: NARROW_COLS,
    wideCols: WIDE_COLS,
    families,
    extra: extra.map((e, i) => ({ ...e, family: `extra${i}` })),
  };
  return results;
}

module.exports = { collect, NARROW_COLS, WIDE_COLS };
