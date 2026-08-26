import { create } from 'zustand'

/**
 * chatDraftStore lets a surface outside the chat pane push text into the
 * composer without owning it.
 *
 * The Design Studio is the reason it exists: clicking an element in the preview
 * has to become prompt context ("the element I mean is design://n42"), and the
 * composer is the only place that text belongs. The store is a one-shot mailbox
 * rather than shared state so the composer stays the single owner of its own
 * value: a producer drops text in, the composer takes it out and appends it.
 */
interface ChatDraftStore {
  /** Text waiting to be appended to the composer, null when there is none. */
  pendingInsert: string | null
  /** Queue text for the composer. */
  insertIntoDraft: (text: string) => void
  /** Take the queued text, clearing it. Returns null when the mailbox is empty. */
  takeDraftInsert: () => string | null
}

export const useChatDraftStore = create<ChatDraftStore>((set, get) => ({
  pendingInsert: null,

  insertIntoDraft: (text: string) => {
    const trimmed = text.trim()
    if (!trimmed) return
    // A second click before the composer has read the first must not lose it:
    // queued fragments accumulate on separate lines.
    const pending = get().pendingInsert
    set({ pendingInsert: pending ? `${pending}\n${trimmed}` : trimmed })
  },

  takeDraftInsert: () => {
    const pending = get().pendingInsert
    if (pending === null) return null
    set({ pendingInsert: null })
    return pending
  },
}))
