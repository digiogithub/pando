// Run-sequence bookkeeping for the chat view.
//
// The server numbers every agent run of a session (`run_seq`, monotonic, never
// reused). The client remembers the last number it streamed or loaded per
// session and reattaches to the session stream whenever the server reports a
// greater one, whether that run is still going or already over. That is what
// makes a run the client did not start (an idle session resumed after a
// delegated subagent) show up without navigation, however short it was.

/**
 * Value stored as "last seen" when the client knows its view of the session is
 * incomplete: the session was opened while a run was in flight, or a stream
 * dropped before its `done`. Below every real sequence (including 0), so the
 * next poll triggers a reattach.
 */
export const RUN_SEQ_STALE = -1

export interface ReattachInput {
  /** run_seq reported by the server (pending poll / session list), if known */
  serverSeq?: number
  /** last run_seq this client streamed or loaded; undefined until history is loaded */
  seenSeq?: number
  /** true while this client has a stream open */
  streaming: boolean
}

/**
 * True when the session has a run this client has not shown. Never while a
 * stream is open (that stream will announce its own sequence) and never before
 * the history is loaded (an unknown baseline would replay what is already there).
 */
export function shouldReattach({ serverSeq, seenSeq, streaming }: ReattachInput): boolean {
  if (streaming) return false
  if (serverSeq === undefined || seenSeq === undefined) return false
  return serverSeq > seenSeq
}
