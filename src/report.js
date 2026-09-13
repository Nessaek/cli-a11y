'use strict';

// The report.
//
// This tool would have no standing if its own output failed the rules it
// enforces, so: colour only ever reinforces a word that already says the same
// thing, NO_COLOR and TERM=dumb are honoured, nothing is dimmed, nothing is
// redrawn in place, and everything wraps to the real terminal width.

const SEVERITY_WEIGHT = { critical: 25, serious: 10, moderate: 4, minor: 1 };
const SEVERITY_ORDER = ['critical', 'serious', 'moderate', 'minor'];

const DIMENSIONS = {
  vision: 'Vision and screen readers',
  motor: 'Interaction and input',
  clarity: 'Comprehension',
};

function styler(enabled) {
  const wrap = (code) => (s) => (enabled ? `\x1b[${code}m${s}\x1b[0m` : String(s));
  return {
    bold: wrap(1),
    red: wrap(91),
    yellow: wrap(93),
    green: wrap(92),
    cyan: wrap(96),
    // Deliberately not a "dim" helper. Faint text is one of the things this
    // tool reports as a fault; it has no business using it.
    plain: (s) => String(s),
  };
}

function colourEnabled(opts) {
  if (opts.color === false) return false;
  if (opts.color === true) return true;
  if (process.env.NO_COLOR) return false;
  if (process.env.TERM === 'dumb') return false;
  return Boolean(process.stdout.isTTY);
}

function wrapText(text, width, indent = '') {
  const out = [];
  for (const para of String(text).split('\n')) {
    let line = indent;
    for (const word of para.split(/\s+/).filter(Boolean)) {
      if (line.length > indent.length && line.length + 1 + word.length > width) {
        out.push(line);
        line = indent + word;
      } else {
        line = line.length > indent.length ? `${line} ${word}` : indent + word;
      }
    }
    out.push(line);
  }
  return out;
}

function score(findings) {
  const penalty = findings
    .filter((f) => f.status === 'fail')
    .reduce((a, f) => a + (SEVERITY_WEIGHT[f.severity] || 0), 0);
  return Math.max(0, 100 - penalty);
}

function grade(n) {
  if (n >= 90) return 'good';
  if (n >= 70) return 'workable, with gaps';
  if (n >= 45) return 'hard going';
  return 'largely unusable';
}

function summarise(findings) {
  const counts = { critical: 0, serious: 0, moderate: 0, minor: 0, pass: 0, skip: 0 };
  for (const f of findings) {
    if (f.status === 'pass') counts.pass++;
    else if (f.status === 'skip') counts.skip++;
    else counts[f.severity] = (counts[f.severity] || 0) + 1;
  }
  return counts;
}

/** Does anything reach the threshold that should fail a build? */
function shouldFail(findings, level) {
  if (level === 'none') return false;
  const cut = SEVERITY_ORDER.indexOf(level);
  if (cut < 0) return false;
  return findings.some((f) => f.status === 'fail'
    && SEVERITY_ORDER.indexOf(f.severity) <= cut);
}

function render(result, opts = {}) {
  const { findings, target, meta } = result;
  const colour = colourEnabled(opts);
  const c = styler(colour);
  const width = Math.max(40, Math.min(opts.width || process.stdout.columns || 80, 100));
  const lines = [];
  const counts = summarise(findings);
  const total = score(findings);

  const label = {
    critical: c.red('CRITICAL'),
    serious: c.red('SERIOUS '),
    moderate: c.yellow('MODERATE'),
    minor: c.yellow('MINOR   '),
  };

  lines.push('');
  lines.push(c.bold(`Accessibility report for: ${target.cmd} ${target.args.join(' ')}`.trim()));
  lines.push('');
  lines.push(`Score ${c.bold(`${total}/100`)} — ${grade(total)}`);
  lines.push(`${counts.critical} critical, ${counts.serious} serious, ${counts.moderate} moderate, `
    + `${counts.minor} minor, ${counts.pass} passed, ${counts.skip} not applicable`);
  if (!meta.pty) {
    lines.push('');
    lines.push(...wrapText('Note: no usable script(1) on this machine, so every terminal probe was '
      + 'skipped. Only the piped behaviour was graded, which misses most colour and '
      + 'animation faults.', width));
  }

  for (const [dim, title] of Object.entries(DIMENSIONS)) {
    const group = findings.filter((f) => f.dimension === dim);
    if (!group.length) continue;
    const fails = group.filter((f) => f.status === 'fail');
    const dimScore = score(group);

    lines.push('');
    lines.push(c.bold(title) + `  —  ${fails.length} of ${group.length} checks failed, score ${dimScore}/100`);
    lines.push('-'.repeat(Math.min(width, 72)));

    const ordered = [
      ...SEVERITY_ORDER.flatMap((s) => fails.filter((f) => f.severity === s)),
      ...(opts.quiet ? [] : group.filter((f) => f.status !== 'fail')),
    ];

    let separated = false;
    for (const f of ordered) {
      if (f.status === 'fail') {
        lines.push('');
        lines.push(`${label[f.severity] || f.severity}  ${c.bold(f.title)}  [${f.id}]`);
        lines.push(...wrapText(f.detail, width, '          '));
        for (const e of f.evidence || []) {
          lines.push(...wrapText(`· ${e}`, width, '          '));
        }
        if (f.remedy) {
          lines.push(...wrapText(`${c.cyan('Fix:')} ${f.remedy}`, width, '          '));
        }
      } else {
        // One blank line separates the failures from the roll-call of what
        // passed, so the two never run together when read aloud.
        if (!separated) { lines.push(''); separated = true; }
        if (f.status === 'pass') {
          lines.push(`${c.green('PASS')}      ${f.title}`);
        } else {
          const [first, ...more] = wrapText(`${f.title} — ${f.detail}`, width - 10);
          lines.push(`N/A       ${first}`);
          more.forEach((l) => lines.push(`          ${l}`));
        }
      }
    }
  }

  lines.push('');
  return lines.join('\n');
}

function toJson(result) {
  const { findings, target, meta } = result;
  return JSON.stringify({
    target,
    score: score(findings),
    grade: grade(score(findings)),
    counts: summarise(findings),
    dimensions: Object.fromEntries(Object.keys(DIMENSIONS).map((d) => {
      const g = findings.filter((f) => f.dimension === d);
      return [d, { score: score(g), checks: g.length, failed: g.filter((f) => f.status === 'fail').length }];
    })),
    meta: { pty: meta.pty, probes: meta.probeCount, durationMs: meta.durationMs },
    findings: findings.map((f) => ({
      id: f.id,
      dimension: f.dimension,
      status: f.status,
      severity: f.severity,
      title: f.title,
      detail: f.detail,
      evidence: f.evidence,
      remedy: f.remedy,
    })),
  }, null, 2);
}

module.exports = {
  render, toJson, score, grade, summarise, shouldFail,
  SEVERITY_ORDER, SEVERITY_WEIGHT, DIMENSIONS,
};
