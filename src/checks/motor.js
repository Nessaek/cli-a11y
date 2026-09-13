'use strict';

// Checks for people who cannot produce fast, precise or timed keystrokes:
// switch access, eye tracking, voice control, tremor, RSI, or simply a screen
// reader that has to read a menu before a choice can be made.
//
// The recurring theme is the escape hatch. An interactive prompt is not itself
// a barrier; an interactive prompt with no way to answer it up front is.

const U = require('./util');

const DIM = 'motor';

// How a prompt looks when it is waiting for you.
const PROMPT_TAIL = /(\?|:|»|›|>)\s*$|\[[yYnN]\/[yYnN]\]\s*$|\((?:y\/n|yes\/no)\)\s*[:?]?\s*$/;
const PROMPT_WORDS = /\b(enter|choose|select|confirm|continue|proceed|overwrite|password|username|press\s+(?:any\s+key|enter|return|[a-z]\b)|are you sure|would you like|do you want|type\s+(?:the|your|yes))\b/i;

const BATCH_FLAGS = ['--yes', '--assume-yes', '--non-interactive', '--noninteractive',
  '--no-input', '--no-interaction', '--batch', '--force', '--defaults', '--ci',
  '-y,', '-y ', '--confirm=', '--accept',
  // gcloud, apt and others spell "answer nothing and carry on" as --quiet.
  '--quiet', '-q,'];

const MACHINE_FLAGS = ['--json', '--format', '--porcelain', '--output', '--quiet',
  '--silent', '-o,', '--plain', '--no-color', '--machine'];

const ARROW_MENU = /(use\s+arrow\s+keys|↑\s*\/?\s*↓|↓\s*\/?\s*↑|arrow keys to (?:move|navigate|select)|space\s+to\s+(?:select|toggle)|<space>|j\/k\s+to|press\s+<?(?:tab|space)>?\s+to)/i;

const TIMED = /\b(?:in|within|after)\s+\d+\s*(?:second|sec|s\b|minute)|\btimed?\s*out\s+in\b|\bauto(?:matically)?\s+(?:continu|proceed|select|cancel)/i;

/** Does this run look like it stopped and waited for a human? */
function promptSignals(probe) {
  if (!U.ok(probe)) return null;
  // A pager also stops and waits, and its prompt is a bare colon. That is not
  // the CLI asking the user a question.
  if (U.isPaging(probe)) {
    return { looksLikePrompt: false, paging: true, tail: false, words: false, last: '', text: '' };
  }
  const lines = U.visibleLines(probe.raw).filter((l) => l.trim());
  const last = lines[lines.length - 1] || '';
  const all = lines.join('\n');
  const tail = PROMPT_TAIL.test(last);
  const words = PROMPT_WORDS.test(all);
  return {
    looksLikePrompt: (tail && words) || (probe.timedOut && (tail || words)),
    tail,
    words,
    last,
    text: all,
  };
}

function checkInteractiveHang(probes) {
  const bare = probes.bare;
  if (!U.ok(bare)) return [];
  const sig = promptSignals(bare);
  const help = U.helpText(probes);
  const escapes = U.mentions(help, BATCH_FLAGS);

  if (!bare.timedOut) {
    return [U.pass({
      id: 'M-INTERACTIVE-HANG', dimension: DIM, title: 'Completes without waiting for input',
      detail: `A bare invocation finished on its own in ${(bare.durationMs / 1000).toFixed(1)}s.`,
    })];
  }

  if (sig.paging) {
    return [U.pass({
      id: 'M-INTERACTIVE-HANG', dimension: DIM, title: 'Does not wait for input',
      detail: 'The bare invocation handed off to a pager rather than prompting. Reported separately as V-PAGER.',
    })];
  }

  if (!sig.looksLikePrompt) {
    return [U.skip({
      id: 'M-INTERACTIVE-HANG', dimension: DIM, title: 'Waiting for input',
      detail: 'A bare invocation did not finish within the timeout, but nothing in its output looks like a prompt. It is probably long-running or a filter rather than interactive; re-run with --cmd to exercise a command that terminates.',
    })];
  }

  return [U.fail({
    id: 'M-INTERACTIVE-HANG',
    dimension: DIM,
    title: escapes.length ? 'Prompts for input, but an escape hatch exists' : 'Prompts for input with no way to answer up front',
    severity: escapes.length ? 'minor' : 'serious',
    detail: escapes.length
      ? 'The CLI stops and waits for a human, but the help does advertise a non-interactive mode.'
      : 'The CLI stops and waits for a human, and the help advertises no flag that supplies the answers up front. Anyone driving this by voice control, switch access or a script is stuck at the prompt, and a screen reader user gets no announcement that input is expected at all.',
    evidence: [
      `last line before the timeout: ${JSON.stringify(U.excerpt(sig.last, 60))}`,
      escapes.length ? `help mentions: ${escapes.join(', ')}` : 'help mentions none of --yes, --non-interactive, --no-input, --batch, --force',
    ],
    remedy: escapes.length
      ? 'Name the flag in the prompt itself, so someone who cannot answer it can discover the way out without leaving the terminal.'
      : 'Accept every prompted answer as a flag or environment variable, and default to non-interactive when stdin is not a terminal.',
  })];
}

function checkPipePrompt(probes) {
  const p = probes.barePipe;
  if (!U.ok(p)) return [];
  const sig = promptSignals(p);
  if (p.timedOut && sig.looksLikePrompt) {
    return [U.fail({
      id: 'M-PIPE-PROMPT',
      dimension: DIM,
      title: 'Prompts even when stdin is not a terminal',
      severity: 'serious',
      detail: 'The CLI waits for interactive input with stdin redirected. Nothing can drive it — not a script, not CI, not an assistive tool that automates a workflow — and it simply hangs with no indication why.',
      evidence: [`last line before the timeout: ${JSON.stringify(U.excerpt(sig.last, 60))}`],
      remedy: 'Check isatty(stdin). When it is false, either take the default or fail with a message naming the flag that supplies the answer.',
    })];
  }
  return [U.pass({
    id: 'M-PIPE-PROMPT', dimension: DIM, title: 'Does not prompt when stdin is redirected',
    detail: 'Piped invocation did not block on a prompt.',
  })];
}

function checkBatchFlags(probes) {
  const help = U.helpText(probes);
  if (!help) return [];
  const found = U.mentions(help, BATCH_FLAGS);
  const interactive = [probes.bare, probes.barePipe]
    .some((p) => U.ok(p) && promptSignals(p).looksLikePrompt);

  if (found.length) {
    return [U.pass({
      id: 'M-BATCH-FLAG', dimension: DIM, title: 'Offers a non-interactive mode',
      detail: `Help advertises ${found.join(', ')}.`,
    })];
  }
  if (!interactive) {
    return [U.pass({
      id: 'M-BATCH-FLAG', dimension: DIM, title: 'Non-interactive by nature',
      detail: 'No prompting was observed, so no batch flag is needed.',
    })];
  }
  return [U.fail({
    id: 'M-BATCH-FLAG',
    dimension: DIM,
    title: 'No documented non-interactive mode',
    severity: 'moderate',
    detail: 'Prompting was observed but the help advertises no flag to answer it in advance. The workaround people are left with is piping "yes", which is fragile and undiscoverable.',
    evidence: ['searched help for: --yes, --assume-yes, --non-interactive, --no-input, --batch, --force, --defaults'],
    remedy: 'Add a documented flag per prompt, plus one blanket --yes. Mention them in the help near the command that prompts.',
  })];
}

function checkArrowMenus(probes) {
  const hits = [];
  for (const [id, p] of Object.entries(probes)) {
    if (id === '_meta' || !U.ok(p)) continue;
    const text = U.visibleText(p.raw);
    if (ARROW_MENU.test(text)) {
      const line = text.split('\n').find((l) => ARROW_MENU.test(l)) || '';
      hits.push(`${id}: ${U.excerpt(line, 60)}`);
    }
  }
  if (!hits.length) {
    return [U.pass({
      id: 'M-ARROW-MENU', dimension: DIM, title: 'No keystroke-driven menus',
      detail: 'No arrow-key or space-bar selection menus were seen.',
    })];
  }
  return [U.fail({
    id: 'M-ARROW-MENU',
    dimension: DIM,
    title: 'Selection menu driven by raw keystrokes',
    severity: 'moderate',
    detail: 'A menu navigated by arrow keys or space puts the CLI into raw mode. Screen readers get no announcement when the highlighted row changes, so the user is moving a cursor they cannot hear; voice and switch users have to issue one command per row.',
    evidence: hits.slice(0, 4),
    remedy: 'Always offer the same choice as a flag argument, and accept a typed number or name as well as arrow keys. Print the full list first so it can be read before choosing.',
  })];
}

function checkTimedPrompts(probes) {
  const hits = [];
  for (const [id, p] of Object.entries(probes)) {
    if (id === '_meta' || !U.ok(p)) continue;
    const text = U.visibleText(p.raw);
    if (!TIMED.test(text)) continue;
    const line = text.split('\n').find((l) => TIMED.test(l)) || '';
    // Only interesting where something is being asked of the user.
    if (!PROMPT_WORDS.test(text) && !/cancel|abort|continu|proceed/i.test(line)) continue;
    hits.push(`${id}: ${U.excerpt(line, 70)}`);
  }
  if (!hits.length) {
    return [U.pass({
      id: 'M-TIMED-PROMPT', dimension: DIM, title: 'No time-limited prompts',
      detail: 'Nothing appears to act on a countdown.',
    })];
  }
  return [U.fail({
    id: 'M-TIMED-PROMPT',
    dimension: DIM,
    title: 'Prompt acts on a time limit',
    severity: 'serious',
    detail: 'Something proceeds or cancels on a timer. A screen reader may not have finished reading the question by then, and switch or voice input can take far longer than the allowance. WCAG treats an unextendable time limit as a failure for exactly this reason.',
    evidence: hits.slice(0, 4),
    remedy: 'Wait indefinitely when stdin is a terminal, or make the timeout configurable and generous. Never let a countdown choose the destructive option.',
  })];
}

function checkMachineOutput(probes) {
  const help = U.helpText(probes);
  if (!help) return [];
  const found = U.mentions(help, MACHINE_FLAGS);
  if (found.length) {
    return [U.pass({
      id: 'M-MACHINE-OUTPUT', dimension: DIM, title: 'Output can be consumed by other tools',
      detail: `Help advertises ${found.join(', ')}.`,
    })];
  }
  return [U.fail({
    id: 'M-MACHINE-OUTPUT',
    dimension: DIM,
    title: 'No machine-readable output mode',
    severity: 'minor',
    detail: 'The help advertises no structured or quiet output. Scripting is how many disabled users avoid a difficult interface altogether — wrapping the tool once in something they can drive — and that route is closed without a stable output format.',
    evidence: ['searched help for: --json, --format, --porcelain, --output, --quiet, --plain'],
    remedy: 'Add --json, or at minimum a stable line-oriented --quiet mode that omits decoration.',
  })];
}

function checks(probes) {
  return [
    ...checkInteractiveHang(probes),
    ...checkPipePrompt(probes),
    ...checkBatchFlags(probes),
    ...checkArrowMenus(probes),
    ...checkTimedPrompts(probes),
    ...checkMachineOutput(probes),
  ];
}

module.exports = { checks, promptSignals };
