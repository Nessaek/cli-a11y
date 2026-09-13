'use strict';

// Checks for comprehension: help you can find your way around, errors that say
// what to do next, exit codes that mean what they claim.
//
// This is the dimension people argue is "not really accessibility". It is the
// one that decides whether someone with a cognitive disability, or reading in a
// second language, or hearing the output one line at a time through speech,
// can use the tool at all.

const U = require('./util');

const DIM = 'clarity';

function checkHelpExists(probes) {
  const p = probes.help || probes.helpPipe;
  const meta = probes._meta;
  if (!U.ok(p)) return [];
  const text = U.visibleText(p.raw).trim();

  // Paged help has not failed to return; it is waiting in less. Judge its
  // content by the piped run, which no pager touches.
  if (p.timedOut && U.isPaging(p)) {
    const piped = U.visibleText((probes.helpPipe && probes.helpPipe.raw) || '').trim();
    if (piped.length > 20) {
      return [U.pass({
        id: 'C-HELP-EXISTS', dimension: DIM, title: 'Help is available',
        detail: `${meta.helpFlag} printed ${piped.split('\n').length} lines, through a pager on a terminal. Paging itself is reported as V-PAGER.`,
      })];
    }
  }

  if (p.timedOut) {
    return [U.fail({
      id: 'C-HELP-EXISTS',
      dimension: DIM,
      title: `${meta.helpFlag} does not return`,
      severity: 'critical',
      detail: `Running ${meta.helpFlag} did not terminate. Help is the one thing a user reaches for when stuck, and it is the first thing a screen reader user runs on an unfamiliar tool.`,
      remedy: 'Make the help flag short-circuit everything else and exit immediately.',
    })];
  }
  if (text.length < 20) {
    return [U.fail({
      id: 'C-HELP-EXISTS',
      dimension: DIM,
      title: `${meta.helpFlag} produces no help`,
      severity: 'critical',
      detail: `Running ${meta.helpFlag} produced ${text.length} characters of output. There is no way to discover what the tool does without leaving the terminal.`,
      evidence: [`exit code ${p.code}`, text ? `output: ${U.excerpt(text)}` : 'no output at all'],
      remedy: 'Print a usage summary, the available options and at least one example.',
    })];
  }
  return [U.pass({
    id: 'C-HELP-EXISTS', dimension: DIM, title: 'Help is available',
    detail: `${meta.helpFlag} printed ${text.split('\n').length} lines.`,
  })];
}

function checkHelpExit(probes) {
  const p = probes.helpPipe;
  const meta = probes._meta;
  if (!U.ok(p) || p.timedOut) return [];
  if (p.code === 0) {
    return [U.pass({
      id: 'C-HELP-EXIT', dimension: DIM, title: 'Help exits successfully',
      detail: `${meta.helpFlag} exited 0.`,
    })];
  }
  return [U.fail({
    id: 'C-HELP-EXIT',
    dimension: DIM,
    title: 'Help exits with an error code',
    severity: 'minor',
    detail: `${meta.helpFlag} exited ${p.code}. Asking for help is not a failure, and a non-zero code here breaks shell chains and makes wrappers report a problem that did not happen.`,
    remedy: 'Exit 0 when help was explicitly requested. Reserve non-zero for help printed because the invocation was wrong.',
  })];
}

function checkHelpStream(probes) {
  const p = probes.helpPipe;
  const meta = probes._meta;
  if (!U.ok(p) || p.timedOut) return [];
  const out = U.visibleText(p.stdout).trim();
  const err = U.visibleText(p.stderr).trim();
  if (out.length > 20) {
    return [U.pass({
      id: 'C-HELP-STREAM', dimension: DIM, title: 'Help goes to stdout',
      detail: 'Help can be piped, paged and saved.',
    })];
  }
  if (err.length > 20) {
    return [U.fail({
      id: 'C-HELP-STREAM',
      dimension: DIM,
      title: 'Help printed to stderr',
      severity: 'moderate',
      detail: `${meta.helpFlag} writes to stderr, so "cmd --help | less" and "cmd --help > notes.txt" both come back empty. Paging is how someone reading by speech gets through a long help text at their own pace.`,
      evidence: [`stdout: ${out.length} chars, stderr: ${err.length} chars`],
      remedy: 'Requested help goes to stdout. Only help printed in response to a usage error belongs on stderr.',
    })];
  }
  return [];
}

function checkVersion(probes) {
  const p = probes.version;
  const meta = probes._meta;
  if (!U.ok(p)) return [];
  const text = U.visibleText(p.raw).trim();
  const looksVersioned = /\d+\.\d+/.test(text) || /version/i.test(text);
  if (p.code === 0 && looksVersioned && text.length < 400) {
    return [U.pass({
      id: 'C-VERSION', dimension: DIM, title: 'Reports its version',
      detail: U.excerpt(text, 60),
    })];
  }
  return [U.fail({
    id: 'C-VERSION',
    dimension: DIM,
    title: `${meta.versionFlag} does not report a version`,
    severity: 'minor',
    detail: 'No recognisable version string. Someone who needs help — from a colleague, a forum, or a support desk they can reach — has to start by working out which version they have.',
    evidence: [`exit code ${p.code}`, text ? `output: ${U.excerpt(text)}` : 'no output'],
    remedy: 'Print the version and exit 0.',
  })];
}

function checkErrorExit(probes) {
  const p = probes.badFlag;
  const meta = probes._meta;
  if (!U.ok(p) || p.timedOut) return [];
  if (p.code !== 0) {
    return [U.pass({
      id: 'C-ERROR-EXIT', dimension: DIM, title: 'Bad input fails loudly',
      detail: `An unrecognised flag exited ${p.code}.`,
    })];
  }
  return [U.fail({
    id: 'C-ERROR-EXIT',
    dimension: DIM,
    title: 'Bad input exits successfully',
    severity: 'serious',
    detail: `Passing ${meta.badFlag} exited 0. A typo is silently accepted, so the only signal that something went wrong is whatever the user can spot in the output — which is no signal at all if they cannot see it, or are reading it one line at a time.`,
    remedy: 'Reject unknown options with a non-zero exit code.',
  })];
}

function checkErrorStream(probes) {
  const p = probes.badFlag;
  if (!U.ok(p) || p.timedOut) return [];
  const err = U.visibleText(p.stderr).trim();
  const out = U.visibleText(p.stdout).trim();
  if (err.length > 0) {
    return [U.pass({
      id: 'C-ERROR-STREAM', dimension: DIM, title: 'Errors go to stderr',
      detail: 'The error is visible even when stdout is redirected.',
    })];
  }
  if (out.length > 0) {
    return [U.fail({
      id: 'C-ERROR-STREAM',
      dimension: DIM,
      title: 'Errors printed to stdout',
      severity: 'moderate',
      detail: 'The error message went to stdout. Redirect the output to a file — which is exactly what someone does before reading it in an editor with a screen reader — and the error vanishes into the file instead of reaching the terminal.',
      evidence: [`stdout: ${U.excerpt(out, 60)}`],
      remedy: 'Diagnostics go to stderr; only the tool\'s actual product goes to stdout.',
    })];
  }
  return [U.fail({
    id: 'C-ERROR-STREAM',
    dimension: DIM,
    title: 'Bad input produces no message',
    severity: 'serious',
    detail: 'An unrecognised flag produced no output on either stream. The user gets nothing to act on.',
    remedy: 'Always say what was rejected and why.',
  })];
}

function checkErrorActionable(probes) {
  const p = probes.badFlag;
  const meta = probes._meta;
  if (!U.ok(p) || p.timedOut) return [];
  const text = U.visibleText(p.raw).trim();
  if (!text) return [];

  const namesInput = text.includes(meta.badFlag) || text.includes(meta.badFlag.replace(/^--/, ''));
  // "Where to look next" takes many shapes: --help, a usage line, a spelling
  // suggestion, a documentation URL, or simply naming a help command. Matching
  // only the first few spellings punishes tools that do the right thing in
  // their own words.
  const pointsOn = /--help\b|(?:^|\s)-h(?:\s|$)|usage:|did you mean|maybe you meant|\bhelp\b|https?:\/\/|for more (?:info|information|details)/i
    .test(text);
  const wall = text.split('\n').length > 40;

  const missing = [];
  if (!namesInput) missing.push('it does not repeat back the input it rejected');
  if (!pointsOn) missing.push('it does not say where to look next');
  if (wall) missing.push(`it dumps ${text.split('\n').length} lines of help on top of the message`);

  if (!missing.length) {
    return [U.pass({
      id: 'C-ERROR-ACTIONABLE', dimension: DIM, title: 'Errors are actionable',
      detail: 'The message names the rejected input and points the user somewhere.',
      evidence: [U.excerpt(text.split('\n').find((l) => l.trim()) || '', 70)],
    })];
  }

  return [U.fail({
    id: 'C-ERROR-ACTIONABLE',
    dimension: DIM,
    title: 'Error message is not actionable',
    severity: missing.length > 1 ? 'moderate' : 'minor',
    detail: `The message for an unrecognised flag is incomplete: ${missing.join('; ')}. Read aloud, an error that does not name the offending input and does not say what to do next leaves nothing to act on.`,
    evidence: [`first line: ${U.excerpt(text.split('\n').find((l) => l.trim()) || '', 70)}`],
    remedy: 'Quote the input you rejected, suggest the nearest valid option, and name the one command that lists them all.',
  })];
}

function checkHelpStructure(probes) {
  const help = U.helpText(probes);
  if (!help) return [];
  const hasUsage = /^\s*(usage|synopsis)\b/im.test(help);
  const hasOptions = /^\s*(options|flags|arguments|commands|subcommands)\b/im.test(help)
    || /^\s*-{1,2}[a-z]/im.test(help);
  const hasExample = /^\s*(examples?)\b/im.test(help)
    || /^\s{2,}\$\s+\S/m.test(help);

  const missing = [];
  if (!hasUsage) missing.push('no usage line');
  if (!hasOptions) missing.push('no options section');

  const findings = [];
  if (missing.length) {
    findings.push(U.fail({
      id: 'C-HELP-STRUCTURE',
      dimension: DIM,
      title: 'Help has no predictable structure',
      severity: 'moderate',
      detail: `Help text is missing landmarks users navigate by: ${missing.join(', ')}. Reading by speech is linear, so a heading that can be searched for is the only way to skip to the relevant part.`,
      remedy: 'Follow the conventional shape: a usage line, then a description, then options, then examples, each under a heading.',
    }));
  } else {
    findings.push(U.pass({
      id: 'C-HELP-STRUCTURE', dimension: DIM, title: 'Help follows the conventional shape',
      detail: 'Usage line and options section both present.',
    }));
  }

  findings.push(hasExample
    ? U.pass({
      id: 'C-HELP-EXAMPLES', dimension: DIM, title: 'Help includes examples',
      detail: 'At least one worked invocation is shown.',
    })
    : U.fail({
      id: 'C-HELP-EXAMPLES',
      dimension: DIM,
      title: 'Help includes no examples',
      severity: 'minor',
      detail: 'No example invocation. An example is a template to copy; assembling one from a list of options is a working-memory task the rest of the help does not require.',
      remedy: 'Show two or three complete commands covering the common cases.',
    }));

  return findings;
}

function checkReadability(probes) {
  const help = U.helpText(probes);
  if (!help) return [];
  const lines = help.split('\n');
  const longLines = lines.filter((l) => l.trim().length > 100);
  const words = help.split(/\s+/).filter(Boolean);
  const sentences = help.split(/[.!?](?:\s|$)/).filter((s) => s.trim().split(/\s+/).length > 3);
  const avgSentence = sentences.length
    ? sentences.reduce((a, s) => a + s.trim().split(/\s+/).length, 0) / sentences.length
    : 0;

  const problems = [];
  if (longLines.length > 3) {
    problems.push(`${longLines.length} lines run past 100 characters`);
  }
  if (avgSentence > 25) {
    problems.push(`sentences average ${avgSentence.toFixed(0)} words`);
  }
  if (lines.length > 250) {
    problems.push(`${lines.length} lines of help arrive in one block`);
  }

  if (!problems.length) {
    return [U.pass({
      id: 'C-READABILITY', dimension: DIM, title: 'Help is readable',
      detail: `${lines.length} lines, ${words.length} words, sentences averaging ${avgSentence.toFixed(0)} words.`,
    })];
  }

  return [U.fail({
    id: 'C-READABILITY',
    dimension: DIM,
    title: 'Help is hard to read',
    severity: 'minor',
    detail: `${problems.join('; ')}. Long lines and long sentences cost the most for readers with dyslexia or a cognitive disability, and for anyone hearing the text rather than scanning it.`,
    evidence: longLines.slice(0, 3).map((l) => `${l.trim().length} chars: ${U.excerpt(l, 60)}`),
    remedy: 'Wrap to the terminal width, keep sentences short, and split a long help into subcommand help pages.',
  })];
}

function checks(probes) {
  return [
    ...checkHelpExists(probes),
    ...checkHelpExit(probes),
    ...checkHelpStream(probes),
    ...checkVersion(probes),
    ...checkErrorExit(probes),
    ...checkErrorStream(probes),
    ...checkErrorActionable(probes),
    ...checkHelpStructure(probes),
    ...checkReadability(probes),
  ];
}

module.exports = { checks };
