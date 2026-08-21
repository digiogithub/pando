import { create } from 'zustand'

export type FileType = 'text' | 'image' | 'pdf'

export interface OpenFile {
  path: string
  content: string
  language: string
  fileType: FileType
  isDirty: boolean
  cursorLine: number
  cursorCol: number
}

interface EditorStore {
  openFiles: OpenFile[]
  activeFilePath: string | null
  fileTreeExpanded: Record<string, boolean>

  openFile: (path: string, content: string) => void
  openBinaryFile: (path: string, fileType: 'image' | 'pdf') => void
  closeFile: (path: string) => void
  setActiveFile: (path: string) => void
  updateFileContent: (path: string, content: string) => void
  markFileSaved: (path: string) => void
  updateCursor: (path: string, line: number, col: number) => void
  toggleTreeNode: (path: string) => void
}

function detectLanguage(path: string): string {
  const ext = path.split('.').pop()?.toLowerCase() ?? ''
  const map: Record<string, string> = {
    go: 'go',
    ts: 'typescript',
    tsx: 'typescript',
    js: 'javascript',
    jsx: 'javascript',
    py: 'python',
    md: 'markdown',
    json: 'json',
    yaml: 'yaml',
    yml: 'yaml',
    css: 'css',
    html: 'html',
    sh: 'shell',
    bash: 'shell',
    rs: 'rust',
    toml: 'toml',
    sql: 'sql',
    lua: 'lua',
  }
  return map[ext] ?? 'plaintext'
}

export const useEditorStore = create<EditorStore>((set, get) => ({
  openFiles: [],
  activeFilePath: null,
  fileTreeExpanded: {},

  openFile: (path: string, content: string) => {
    const existing = get().openFiles.find((f) => f.path === path)
    if (existing) {
      set({ activeFilePath: path })
      return
    }
    const newFile: OpenFile = {
      path,
      content,
      language: detectLanguage(path),
      fileType: 'text',
      isDirty: false,
      cursorLine: 1,
      cursorCol: 1,
    }
    set((s) => ({
      openFiles: [...s.openFiles, newFile],
      activeFilePath: path,
    }))
  },

  openBinaryFile: (path: string, fileType: 'image' | 'pdf') => {
    const existing = get().openFiles.find((f) => f.path === path)
    if (existing) {
      set({ activeFilePath: path })
      return
    }
    const newFile: OpenFile = {
      path,
      content: '',
      language: 'plaintext',
      fileType,
      isDirty: false,
      cursorLine: 1,
      cursorCol: 1,
    }
    set((s) => ({
      openFiles: [...s.openFiles, newFile],
      activeFilePath: path,
    }))
  },

  closeFile: (path: string) => {
    set((s) => {
      const remaining = s.openFiles.filter((f) => f.path !== path)
      let nextActive = s.activeFilePath
      if (s.activeFilePath === path) {
        nextActive = remaining.length > 0 ? remaining[remaining.length - 1].path : null
      }
      return { openFiles: remaining, activeFilePath: nextActive }
    })
  },

  setActiveFile: (path: string) => set({ activeFilePath: path }),

  updateFileContent: (path: string, content: string) => {
    set((s) => ({
      openFiles: s.openFiles.map((f) =>
        f.path === path ? { ...f, content, isDirty: true } : f
      ),
    }))
  },

  markFileSaved: (path: string) => {
    set((s) => ({
      openFiles: s.openFiles.map((f) =>
        f.path === path ? { ...f, isDirty: false } : f
      ),
    }))
  },

  updateCursor: (path: string, line: number, col: number) => {
    set((s) => ({
      openFiles: s.openFiles.map((f) =>
        f.path === path ? { ...f, cursorLine: line, cursorCol: col } : f
      ),
    }))
  },

  toggleTreeNode: (path: string) => {
    set((s) => ({
      fileTreeExpanded: {
        ...s.fileTreeExpanded,
        [path]: !s.fileTreeExpanded[path],
      },
    }))
  },
}))
