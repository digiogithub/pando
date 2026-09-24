import { useRef, useCallback, useEffect } from 'react'
import MonacoEditor, { OnMount, BeforeMount } from '@monaco-editor/react'
import type * as monacoTypes from 'monaco-editor'
import { useEditorStore } from '@pando/client/stores/editorStore'
import api from '@pando/client/services/api'
import { definePandoMonacoTheme, watchMonacoTheme, PANDO_MONACO_THEME } from './monacoTheme'

interface CodeEditorProps {
  filePath: string
  content: string
  language: string
}

export default function CodeEditor({ filePath, content, language }: CodeEditorProps) {
  const editorRef = useRef<monacoTypes.editor.IStandaloneCodeEditor | null>(null)
  const unsubThemeRef = useRef<(() => void) | null>(null)
  const { updateFileContent, updateCursor, markFileSaved } = useEditorStore()

  const handleBeforeMount: BeforeMount = useCallback((monacoInstance) => {
    definePandoMonacoTheme(monacoInstance, document.documentElement.getAttribute('data-theme') === 'dark')
  }, [])

  const handleMount: OnMount = useCallback(
    (editor, monacoInstance) => {
      editorRef.current = editor

      // Ctrl+S to save using KeyMod and KeyCode from the monaco instance
      editor.addCommand(
        monacoInstance.KeyMod.CtrlCmd | monacoInstance.KeyCode.KeyS,
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

      // Keep the Monaco theme in sync with the app theme (family / mode / accent).
      unsubThemeRef.current = watchMonacoTheme(monacoInstance, (m, dark) => {
        definePandoMonacoTheme(m, dark)
        m.editor.setTheme(PANDO_MONACO_THEME)
      })
    },
    [filePath, markFileSaved, updateCursor]
  )

  useEffect(() => {
    return () => unsubThemeRef.current?.()
  }, [])

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
        theme={PANDO_MONACO_THEME}
        language={language}
        value={content}
        beforeMount={handleBeforeMount}
        onMount={handleMount}
        onChange={handleChange}
        options={{
          fontSize: 13,
          fontFamily: "'JetBrains Mono Variable', 'JetBrains Mono', ui-monospace, monospace",
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
