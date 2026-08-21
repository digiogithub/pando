import { create } from 'zustand'
import api from '../services/api'
import type { Message } from '../types'

export interface FileChange {
  filePath: string
  fileName: string
  additions: number
  removals: number
  /** True when the entry came from agent-vcs only (no inline diff available) */
  fromVcs?: boolean
  /** Accumulated diffs for this file (old -> new pairs) */
  edits: Array<{
    oldString: string
    newString: string
  }>
  lastUpdated: number
}

interface FileChangesStore {
  /** Map of filePath -> FileChange for the current session */
  changes: Record<string, FileChange>

  /** Add or update a file change from a tool_result event */
  addChange: (
    filePath: string,
    additions: number,
    removals: number,
    oldString: string,
    newString: string,
  ) => void

  /** Clear all changes (on session switch or new session) */
  clearChanges: () => void

  /**
   * Rebuild the panel for a session that was just loaded: the SSE stream only
   * exists for the live run, so a page reload or a session switch would show
   * nothing. History (edit/write/patch tool calls) is replayed first, then
   * agent-vcs — when the service is enabled — adds any path the history did not
   * name (patch/multi-file tools, edits from a previous instance).
   */
  hydrateSession: (sessionId: string, messages: Message[]) => Promise<void>
}

interface VcsDiffEntry {
  path: string
  type: 'added' | 'modified' | 'deleted'
}

/** Tools whose input describes a file modification. */
const EDIT_TOOLS = new Set(['edit', 'write', 'patch', 'multiedit'])

function countLines(text: string): number {
  return text ? text.split('\n').length : 0
}

function extractFileName(filePath: string): string {
  return filePath.split('/').pop() ?? filePath
}

export const useFileChangesStore = create<FileChangesStore>((set) => ({
  changes: {},

  addChange: (filePath, additions, removals, oldString, newString) => {
    set((s) => {
      const existing = s.changes[filePath]
      if (existing) {
        return {
          changes: {
            ...s.changes,
            [filePath]: {
              ...existing,
              additions: existing.additions + additions,
              removals: existing.removals + removals,
              edits: [...existing.edits, { oldString, newString }],
              lastUpdated: Date.now(),
            },
          },
        }
      }
      return {
        changes: {
          ...s.changes,
          [filePath]: {
            filePath,
            fileName: extractFileName(filePath),
            additions,
            removals,
            edits: [{ oldString, newString }],
            lastUpdated: Date.now(),
          },
        },
      }
    })
  },

  clearChanges: () => set({ changes: {} }),

  hydrateSession: async (sessionId, messages) => {
    const replayed: Record<string, FileChange> = {}
    const push = (filePath: string, oldString: string, newString: string) => {
      const existing = replayed[filePath]
      const additions = countLines(newString)
      const removals = countLines(oldString)
      if (existing) {
        existing.additions += additions
        existing.removals += removals
        existing.edits.push({ oldString, newString })
        return
      }
      replayed[filePath] = {
        filePath,
        fileName: extractFileName(filePath),
        additions,
        removals,
        edits: [{ oldString, newString }],
        lastUpdated: Date.now(),
      }
    }

    for (const msg of messages) {
      for (const part of msg.content ?? []) {
        if (part.type !== 'tool_call' || !part.tool_name) continue
        if (!EDIT_TOOLS.has(part.tool_name.toLowerCase())) continue
        const input = part.tool_input as
          | { file_path?: string; old_string?: string; new_string?: string; content?: string }
          | undefined
        if (!input?.file_path) continue
        push(
          input.file_path,
          input.old_string ?? '',
          input.new_string ?? input.content ?? '',
        )
      }
    }

    set({ changes: replayed })

    // agent-vcs knows every path touched during the session, including the ones
    // no single tool input names. It answers with an empty diff when disabled.
    try {
      const res = await api.get<{ diff?: VcsDiffEntry[] }>(
        `/api/v1/agentvcs/sessions/${sessionId}/diff`,
      )
      const entries = res.diff ?? []
      if (entries.length === 0) return
      set((s) => {
        const merged = { ...s.changes }
        for (const entry of entries) {
          if (!entry.path || merged[entry.path]) continue
          merged[entry.path] = {
            filePath: entry.path,
            fileName: extractFileName(entry.path),
            additions: 0,
            removals: 0,
            fromVcs: true,
            edits: [],
            lastUpdated: Date.now(),
          }
        }
        return { changes: merged }
      })
    } catch {
      // agent-vcs disabled or unreachable — history replay is enough.
    }
  },
}))
