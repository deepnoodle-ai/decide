/**
 * The built-in decide templates the plugin runs, and the questions it reads
 * from each with the probability of yes that flags it: the template's own
 * flag, so the plugin, `decide runs view`, and `decide templates show` agree.
 * `null` reads a question that is never flagged.
 *
 * TestPluginTemplatesAreBuiltin, a Go test in the decide repository, checks
 * this against the templates, so renaming a template or a question, or
 * changing a flag, fails CI instead of quietly changing a check.
 */
export const TEMPLATES = {
  command: { name: 'command-risk', flags: { severe: 0.8 } },
  content: { name: 'prompt-injection', flags: { injection: 0.6, hidden: 0.6 } },
} as const

/** A template's questions and the probability of yes that flags each. */
export type Flags = Readonly<Record<string, number | null>>
