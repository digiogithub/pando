import { create } from 'zustand'

export interface FileChange {
  filePath: string
  fileName: string
  additions: number
  removals: number
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
}))
