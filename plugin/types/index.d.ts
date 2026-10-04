/** One judgment decide made in this session, as /decide lists it. */
export type Judgment = {
  /** When it was made, in milliseconds since the epoch. */
  at: number
  /** Which check made it, or a judge call. */
  kind: 'command' | 'content' | 'reply' | 'judge'
  /** The command, tool, or reply, shortened to one line. */
  subject: string
  /** The answers, such as "destructive 94%, leak 3%". */
  verdict: string
  /** True when an answer passed the threshold. */
  isFlagged: boolean
}

/** What the band above the prompt shows after decide flags a reply. */
export type Notice = {
  /** The line in bold, such as "This reply may claim more than its tools showed". */
  title: string
  /** The answers behind it, such as "overclaims 98%". */
  detail: string
  /** The prompt the band's button sends Claude. */
  prompt: string
  /** The button's label. */
  action: string
}

/** What a check put on its tool's row in the transcript. */
export type RowVerdict = {
  /** True when an answer passed the threshold. */
  isFlagged: boolean
  /** The flagged answers, such as "destructive 96%"; empty when none was. */
  text: string
}

/** How many judgments the session made, past what `log` keeps. */
export type Totals = { checked: number; flagged: number }

declare module 'claude-code' {
  interface PluginState {
    decide: {
      log: readonly Judgment[]
      notice: Notice | null
      totals: Totals
      /** One per tool row, by its tool_use_id. StateFamily is the module's own. */
      rows: StateFamily<RowVerdict | null>
    }
  }
}
