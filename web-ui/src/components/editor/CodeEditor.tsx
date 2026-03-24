import { useRef, useCallback } from 'react'
import MonacoEditor, { OnMount, BeforeMount } from '@monaco-editor/react'
import type * as monaco from 'monaco-editor'
import { useEditorStore } from '@/stores/editorStore'
import api from '@/services/api'

interface CodeEditorProps {
  filePath: string
  content: string
  language: string
}

const PANDO_DARK_THEME: Parameters<BeforeMount>[0] extends infer T ? T : never = (monacoInstance) => {
  monacoInstance.editor.defineTheme('pando-dark', {
    base: 'vs-dark',
    inherit: true,
    rules: [
      { token: 'comment', foreground: '6c7086', fontStyle: 'italic' },
      { token: 'keyword', foreground: 'cba6f7', fontStyle: 'bold' },
      { token: 'string', foreground: 'a6e3a1' },
      { token: 'number', foreground: 'fab387' },
      { token: 'type', foreground: 'f9e2af' },
      { token: 'variable', foreground: 'cdd6f4' },
      { token: 'function', foreground: '89b4fa' },
      { token: 'operator', foreground: '89dceb' },
      { token: 'delimiter', foreground: 'cdd6f4' },
    ],
    colors: {
      'editor.background': '#1e1e2e',
      'editor.foreground': '#cdd6f4',
      'editor.lineHighlightBackground': '#2a2a3d',
      'editor.selectionBackground': '#3d5985',
      'editor.inactiveSelectionBackground': '#2d3f5a',
      'editorCursor.foreground': '#f5c2e7',
      'editorLineNumber.foreground': '#45475a',
      'editorLineNumber.activeForeground': '#cdd6f4',
      'editorIndentGuide.background': '#313244',
      'editorIndentGuide.activeBackground': '#585b70',
      'editor.selectionHighlightBackground': '#2d3f5a80',
      'editorWidget.background': '#1e1e2e',
      'editorWidget.border': '#313244',
      'editorSuggestWidget.background': '#1e1e2e',
      'editorSuggestWidget.border': '#313244',
      'editorSuggestWidget.selectedBackground': '#313244',
      'scrollbarSlider.background': '#45475a60',
      'scrollbarSlider.hoverBackground': '#585b7080',
      'scrollbarSlider.activeBackground': '#585b70',
    },
  })
}

export default function CodeEditor({ filePath, content, language }: CodeEditorProps) {
  const editorRef = useRef<monaco.editor.IStandaloneCodeEditor | null>(null)
  const { updateFileContent, updateCursor, markFileSaved } = useEditorStore()

  const handleBeforeMount: BeforeMount = useCallback((monacoInstance) => {
    PANDO_DARK_THEME(monacoInstance)
  }, [])

  const handleMount: OnMount = useCallback(
    (editor) => {
      editorRef.current = editor

      // Ctrl+S to save — use KeyMod.CtrlCmd | KeyCode.KeyS (numeric: 2097)
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const monacoGlobal = (window as any).monaco
      const saveKeyBinding: number =
        monacoGlobal?.KeyMod?.CtrlCmd && monacoGlobal?.KeyCode?.KeyS
          ? monacoGlobal.KeyMod.CtrlCmd | monacoGlobal.KeyCode.KeyS
          : 2097
      editor.addCommand(
        saveKeyBinding,
        async () => {
          const currentContent = editor.getValue()
          try {
            await api.put(`/api/v1/files/${filePath}`, { content: currentContent })
            markFileSaved(filePath)
          } catch (err) {
            console.error('Failed to save:', err)
          }
        }
      )

      // Track cursor position
      editor.onDidChangeCursorPosition((e) => {
        updateCursor(filePath, e.position.lineNumber, e.position.column)
      })
    },
    [filePath, markFileSaved, updateCursor]
  )

  const handleChange = useCallback(
    (value: string | undefined) => {
      if (value !== undefined) {
        updateFileContent(filePath, value)
      }
    },
    [filePath, updateFileContent]
  )

  return (
    <div style={{ flex: 1, overflow: 'hidden', minHeight: 0 }}>
      <MonacoEditor
        height="100%"
        width="100%"
        theme="pando-dark"
        language={language}
        value={content}
        beforeMount={handleBeforeMount}
        onMount={handleMount}
        onChange={handleChange}
        options={{
          fontSize: 14,
          fontFamily: "'JetBrains Mono', 'Fira Code', 'Cascadia Code', monospace",
          fontLigatures: true,
          lineNumbers: 'on',
          minimap: { enabled: true, scale: 1 },
          scrollBeyondLastLine: false,
          automaticLayout: true,
          tabSize: 2,
          insertSpaces: true,
          wordWrap: 'off',
          renderWhitespace: 'selection',
          bracketPairColorization: { enabled: true },
          guides: { bracketPairs: true, indentation: true },
          smoothScrolling: true,
          cursorBlinking: 'smooth',
          cursorSmoothCaretAnimation: 'on',
          padding: { top: 8, bottom: 8 },
          renderLineHighlight: 'line',
          occurrencesHighlight: 'singleFile',
          suggest: { showWords: true },
          quickSuggestions: true,
          parameterHints: { enabled: true },
          formatOnPaste: true,
          formatOnType: false,
        }}
      />
    </div>
  )
}
