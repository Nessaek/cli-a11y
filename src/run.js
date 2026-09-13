'use strict';

// Runs the target CLI under controlled conditions.
//
// The interesting failures only appear on a real terminal: most CLIs suppress
// colour, spinners and prompts the moment stdout is a pipe, so piping alone
// would grade a program on output no terminal user ever sees. We get a pty out
// of script(1), which is on every macOS and Linux box, rather than taking on a
// native dependency.
//
// script(1) inspects its own stdin with tcgetattr, which fails outright on the
// socketpair Node hands out for stdio 'pipe'. Every pty run therefore gets stdin
// it can live with: /dev/null for an immediate EOF, a temp file for supplied
// input, and — where the CLI must be left waiting — a real shell pipe fed by a
// sleep, which is the one way to hold the stream open without a native module.

const { spawn, spawnSync } = require('child_process');
const fs = require('fs');
const os = require('os');
const path = require('path');
const crypto = require('crypto');

const DEFAULT_TIMEOUT = 10000;

function shQuote(s) {
  return `'` + String(s).replace(/'/g, `'\\''`) + `'`;
}

function tmpName(ext) {
  return path.join(os.tmpdir(), `cli-a11y-${crypto.randomBytes(6).toString('hex')}${ext}`);
}

/** The argv that runs `cmd` on a pty of a known size. */
function ptyArgv(cmd, args, cols, rows) {
  const inner = `stty cols ${cols} rows ${rows} 2>/dev/null; exec ${[cmd, ...args].map(shQuote).join(' ')}`;
  if (os.platform() === 'linux') {
    return ['script', ['-qec', inner, '/dev/null']];
  }
  // macOS and the BSDs take the command as trailing argv.
  return ['script', ['-q', '/dev/null', '/bin/sh', '-c', inner]];
}

/** Does this machine have a script(1) we know how to drive? */
let ptySupport = null;
function ptyAvailable() {
  if (ptySupport !== null) return ptySupport;
  let fd = null;
  try {
    fd = fs.openSync('/dev/null', 'r');
    const [c, a] = ptyArgv('/bin/echo', ['probe'], 80, 24);
    const probe = spawnSync(c, a, { timeout: 5000, stdio: [fd, 'pipe', 'pipe'] });
    ptySupport = !probe.error && probe.status === 0 && String(probe.stdout).includes('probe');
  } catch {
    ptySupport = false;
  } finally {
    if (fd !== null) { try { fs.closeSync(fd); } catch { /* noop */ } }
  }
  return ptySupport;
}

// The pty echoes the EOF we send as caret notation and then rubs it out, and
// some shells announce xterm's meta mode. None of it came from the CLI.
function stripPtyArtefacts(s) {
  return s
    .replace(/^(?:\x04|\^D)(?:\x08| )*/, '')
    .replace(/\x1b\[\?1034h/g, '');
}

/** A real file descriptor script(1) will accept as stdin. */
function openStdinFd(kind, cleanup) {
  if (kind === 'close') return fs.openSync('/dev/null', 'r');
  const file = tmpName('.in');
  fs.writeFileSync(file, String(kind));
  cleanup.push(() => { try { fs.unlinkSync(file); } catch { /* noop */ } });
  return fs.openSync(file, 'r');
}

/**
 * Execute the CLI once.
 *
 * @param {string}   cmd
 * @param {string[]} args
 * @param {object}   opts
 * @param {boolean}  opts.tty    run on a pty; stdout and stderr merge, as a terminal merges them
 * @param {object}   opts.env    extra environment on top of a scrubbed base
 * @param {number}   opts.cols   real pty width, also exported as COLUMNS
 * @param {string}   opts.stdin  'close' for immediate EOF, 'hold' to stay open and silent, or literal input
 * @returns {Promise<object>} raw and normalised output, exit code, timing
 */
function run(cmd, args = [], opts = {}) {
  const {
    tty = false, env = {}, cols = 80, rows = 24,
    timeout = DEFAULT_TIMEOUT, stdin = 'close', cwd = process.cwd(),
  } = opts;

  // A scrubbed base environment, so the host's own NO_COLOR or FORCE_COLOR
  // cannot leak in and quietly invalidate a probe.
  const base = { ...process.env };
  for (const k of ['NO_COLOR', 'FORCE_COLOR', 'CLICOLOR', 'CLICOLOR_FORCE',
    'TERM', 'COLUMNS', 'LINES', 'CI', 'COLORTERM']) {
    delete base[k];
  }
  const childEnv = {
    ...base,
    TERM: tty ? 'xterm-256color' : 'dumb',
    COLUMNS: String(cols),
    LINES: String(rows),
    ...env,
  };

  return new Promise((resolve) => {
    const started = Date.now();
    const cleanup = [];
    let fd = null;
    let child;
    let detached = false;
    let rcFile = null;

    try {
      if (tty && stdin === 'hold') {
        // A sleep on the left of a pipe is the only stdin script(1) will accept
        // that also stays open and silent, so a CLI waiting for input really
        // waits. It has to be a pipe: script rejects a FIFO outright, and Node's
        // own 'pipe' stdio is a socketpair, which it also rejects.
        //
        // The shell would otherwise wait for that sleep as well, making every
        // run last the full timeout whether it hung or not, so the moment the
        // target exits the group is torn down. Its real exit code goes to a file
        // first, because the teardown destroys it.
        const [c, a] = ptyArgv(cmd, args, cols, rows);
        rcFile = tmpName('.rc');
        const secs = Math.ceil(timeout / 1000) + 2;
        const scriptCmd = [c, ...a].map(shQuote).join(' ');
        const pipeline = `sleep ${secs} | { ${scriptCmd}; echo $? > ${shQuote(rcFile)}; kill -TERM 0; }`;
        cleanup.push(() => { try { fs.unlinkSync(rcFile); } catch { /* already gone */ } });
        detached = true;
        child = spawn('/bin/sh', ['-c', pipeline],
          { env: childEnv, cwd, stdio: ['ignore', 'pipe', 'pipe'], detached: true });
      } else if (tty) {
        fd = openStdinFd(stdin, cleanup);
        const [c, a] = ptyArgv(cmd, args, cols, rows);
        child = spawn(c, a, { env: childEnv, cwd, stdio: [fd, 'pipe', 'pipe'] });
      } else {
        child = spawn(cmd, args, { env: childEnv, cwd, stdio: ['pipe', 'pipe', 'pipe'] });
      }
    } catch (err) {
      if (fd !== null) { try { fs.closeSync(fd); } catch { /* noop */ } }
      cleanup.forEach((f) => f());
      resolve({
        cmd, args, tty, cols, env, error: err.message, stdout: '', stderr: '',
        output: '', raw: '', code: null, signal: null, timedOut: false, durationMs: 0,
      });
      return;
    }

    let out = '';
    let err = '';
    let timedOut = false;
    let settled = false;
    let spawnError = null;

    child.stdout.on('data', (d) => { out += d.toString('utf8'); });
    child.stderr.on('data', (d) => { err += d.toString('utf8'); });

    if (!tty) {
      if (stdin === 'close') child.stdin.end();
      else if (stdin !== 'hold') child.stdin.end(String(stdin));
      // 'hold' leaves stdin open and silent, so a CLI that waits for input
      // hangs and the timeout below is itself the finding.
      child.stdin.on('error', () => { /* the CLI may never read it */ });
    }

    const kill = (sig) => {
      try {
        if (detached) process.kill(-child.pid, sig);
        else child.kill(sig);
      } catch { /* already gone */ }
    };

    const killer = setTimeout(() => {
      timedOut = true;
      kill('SIGTERM');
      setTimeout(() => kill('SIGKILL'), 500).unref();
    }, timeout);

    const finish = (code, signal) => {
      if (settled) return;
      settled = true;
      clearTimeout(killer);
      if (!tty) { try { child.stdin.end(); } catch { /* already gone */ } }
      if (fd !== null) { try { fs.closeSync(fd); } catch { /* noop */ } }
      // A held-stdin run is torn down by its own group kill, so the signal and
      // code the shell reports are the teardown's, not the target's. Read this
      // before cleanup, which deletes the file.
      if (rcFile) {
        try {
          const rc = Number(fs.readFileSync(rcFile, 'utf8').trim());
          if (Number.isInteger(rc)) { code = rc; signal = null; }
        } catch { /* the target never got to exit: the timeout killed it */ }
      }
      cleanup.forEach((f) => f());

      const rawOut = tty ? stripPtyArtefacts(out) : out;
      // On a pty the two streams are one, exactly as a terminal presents them.
      const raw = tty ? rawOut : rawOut + err;

      resolve({
        cmd,
        args,
        tty,
        cols,
        env,
        stdout: rawOut,
        stderr: tty ? '' : err,
        raw,
        // Line endings normalised: a pty turns every \n into \r\n, and left in
        // place that would read as a screenful of cursor animation.
        output: raw.replace(/\r\n/g, '\n'),
        code,
        signal,
        timedOut,
        error: spawnError,
        durationMs: Date.now() - started,
      });
    };

    child.on('error', (e) => { spawnError = e.message; finish(null, null); });
    child.on('close', (code, signal) => finish(code, signal));
  });
}

module.exports = { run, ptyAvailable, DEFAULT_TIMEOUT };
