/** A yes-or-no answer: the probability of yes. */
export type Noul = { type: 'noul'; noul: number }

/** A multiple-choice answer: the likeliest option and every option's probability. */
export type Choice = {
  type: 'choice'
  choice: string
  probabilities: Record<string, number>
}

/** A scale answer: the expected level, its names, and each level's probability. */
export type Score = {
  type: 'score'
  score: number
  legend: Record<string, string>
  probabilities: Record<string, number>
}

export type Answer = Noul | Choice | Score

/** One item's answers by question name, or the error decide reported for it. */
export type Item = { answers: Record<string, Answer> } | { error: string }

export type Outcome =
  | { isAnswered: true; items: Item[] }
  | { isAnswered: false; reason: string; isOutdated?: true }

/** Where decide runs from, and with what. */
export type Runner = {
  /** The decide executable. */
  bin: string
  /** DECIDE_HOME for the plugin's runs, kept apart from the person's own. */
  home: string
}

/** One run: the template, a JSON record per item, and the field the model reads. */
export type Run = {
  /** A built-in template's name, or a path to a template folder. */
  template: string
  records: readonly Record<string, unknown>[]
  /** The field of each record the model reads; the whole record when absent. */
  field?: string
  timeoutMs: number
}

/** The most decide reads from stdin as one JSON document, in bytes. */
const MAX_STDIN = 64 << 20

/**
 * The command for a run: its argv and what `$.process.run` takes beside it.
 * The records go as one JSON array, which decide reads up to 64 MiB, and it
 * judges a long item whole, in parts. JSONL would stop at 1 MiB a line.
 * Past 64 MiB, `isTooLarge` is set and the run should not start.
 */
export function request(runner: Runner, run: Run) {
  // "[" alone on the first line makes decide read a document, not JSONL.
  const stdin = `[\n${run.records.map(r => JSON.stringify(r)).join(',\n')}\n]\n`
  const field = run.field ? ['--field', run.field] : []
  return {
    argv: [runner.bin, 'run', run.template, ...field, '--json'],
    // decide reads .decide/templates in its working directory first. Running
    // it from the plugin's own folder keeps a repository's templates out.
    init: { stdin, cwd: runner.home, env: { DECIDE_HOME: runner.home }, timeoutMs: run.timeoutMs },
    isTooLarge: stdin.length * 3 > MAX_STDIN && new TextEncoder().encode(stdin).length > MAX_STDIN,
  }
}

/**
 * Reads what `decide run --json` printed into each record's answers, in the
 * order the records came; or why there are none, so a caller can fail open.
 */
export function outcomeOf(count: number, ran: { exitCode: number; stdout: string; stderr: string }): Outcome {
  const items: Item[] = Array.from({ length: count }, () => ({ error: 'not answered' }))
  for (const line of ran.stdout.split('\n')) {
    if (!line.startsWith('{')) continue
    try {
      const row = JSON.parse(line) as { index?: number; status?: string; answers?: Record<string, Answer>; error?: string }
      if (typeof row.index !== 'number' || row.index < 0 || row.index >= count) continue
      items[row.index] =
        row.status === 'complete' && row.answers ? { answers: row.answers } : { error: row.error ?? row.status ?? 'failed' }
    } catch {
      // A line that is not a result; decide's summary goes to stderr anyway.
    }
  }

  if (items.every(item => 'error' in item)) {
    // decide says why on stderr, in a line that starts "Error:".
    const lines = ran.stderr.split('\n').map(l => l.trim()).filter(Boolean)
    const error = lines.find(l => l.startsWith('Error:'))?.slice('Error:'.length).trim() ?? lines[lines.length - 1]
    if (error && /no template named/i.test(error)) return { isAnswered: false, reason: oneLine(error, 200), isOutdated: true }
    return { isAnswered: false, reason: error ? oneLine(error, 200) : `decide exited with code ${ran.exitCode}` }
  }
  return { isAnswered: true, items }
}

/** The questions whose yes is at least as likely as its flag, likeliest first. */
export function flaggedOf(answers: Record<string, Answer>, flags: Readonly<Record<string, number | null>>): string[] {
  return Object.entries(flags)
    .filter(([name, at]) => at !== null && yes(answers, name) >= at)
    .map(([name]) => name)
    .sort((a, b) => yes(answers, b) - yes(answers, a))
}

/** The questions the plugin reads that have no yes-or-no answer. */
export function missingOf(answers: Record<string, Answer>, flags: Readonly<Record<string, number | null>>): string[] {
  return Object.keys(flags).filter(name => answers[name]?.type !== 'noul')
}

/** The probability of yes, or 0 for a missing or non-yes-or-no answer. */
export function yes(answers: Record<string, Answer>, name: string): number {
  const a = answers[name]
  return a?.type === 'noul' ? a.noul : 0
}

/** A probability as a whole percentage, such as "94%". */
export function pct(p: number): string {
  return `${Math.round(p * 100)}%`
}

/** One answer as a short phrase, such as "yes 94%", "flaky 100%" or "1.9 (Minor–Major)". */
export function phrase(a: Answer): string {
  switch (a.type) {
    case 'noul':
      return a.noul >= 0.5 ? `yes ${pct(a.noul)}` : `no ${pct(1 - a.noul)}`
    case 'choice':
      return `${a.choice} ${pct(a.probabilities[a.choice] ?? 0)}`
    case 'score': {
      const top = Object.keys(a.legend).length - 1
      const level = a.legend[String(Math.round(a.score))] ?? ''
      return `${a.score.toFixed(1)} of ${top}${level ? ` (${level})` : ''}`
    }
  }
}

/** Terminal escape sequences: CSI (colors, cursor moves) and OSC (titles, links). */
const ESCAPES = /\u001b\[[0-9;?]*[ -/]*[@-~]|\u001b\][^\u0007\u001b]*(\u0007|\u001b\\)?/g

/**
 * Characters a terminal acts on or a reader cannot see: C0 and C1 controls,
 * zero-width and bidirectional marks, invisible operators, the byte-order
 * mark, the soft hyphen, and tag characters. Line breaks are handled apart.
 */
const UNSEEN = /[\u0000-\u0009\u000b-\u001f\u007f-\u009f\u00ad\u200b-\u200f\u202a-\u202e\u2060-\u2064\u2066-\u2069\ufeff\u{e0000}-\u{e007f}]+/gu

/**
 * A text on one line, cut to `max` characters, with control characters
 * removed: commands, tool output, and errors are not ours to print as is.
 */
export function oneLine(text: string, max = 80): string {
  const flat = text.replace(ESCAPES, '').replace(UNSEEN, ' ').replace(/\s+/g, ' ').trim()
  return flat.length > max ? flat.slice(0, max - 1) + '…' : flat
}

/**
 * A text as the person should see it in a dialog: its lines kept, control
 * characters removed, and, past `max` characters, a note of how much is not
 * shown, so the end of a long command cannot hide.
 */
export function printable(text: string, max = 2000): string {
  const clean = text.replace(ESCAPES, '').replace(/\r\n?/g, '\n').replace(UNSEEN, ' ').trim()
  if (clean.length <= max) return clean
  return `${clean.slice(0, max)}\n… and ${clean.length - max} more characters, not shown here`
}
