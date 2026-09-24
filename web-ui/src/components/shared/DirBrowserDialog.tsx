import { useCallback, useEffect, useState } from 'react'
import { ArrowUp, Folder, FolderOpen } from '@/components/ui/icons'
import { Button, Dialog, IconButton, Spinner } from '@/components/ui'
import api from '@pando/client/services/api'

interface BrowseResult {
  path: string
  parent: string
  dirs: string[]
}

export default function DirBrowserDialog({
  initialPath,
  onSelect,
  onClose,
}: {
  initialPath?: string
  onSelect: (path: string) => void
  onClose: () => void
}) {
  const [currentPath, setCurrentPath] = useState(initialPath || '~')
  const [result, setResult] = useState<BrowseResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const browse = useCallback(async (path: string) => {
    setLoading(true)
    setError(null)
    try {
      const data = await api.get<BrowseResult>(`/api/v1/fs/browse?path=${encodeURIComponent(path)}`)
      setResult(data)
      setCurrentPath(data.path)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Cannot read directory')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void browse(currentPath)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <Dialog
      open
      onClose={onClose}
      title="Select Directory"
      size="sm"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            icon={<FolderOpen size={14} />}
            disabled={!result}
            onClick={() => {
              if (result) {
                onSelect(result.path)
                onClose()
              }
            }}
          >
            Select
          </Button>
        </>
      }
    >
      {/* Current path */}
      <div className="mb-3 flex items-center gap-2">
        {result?.parent && (
          <IconButton aria-label="Go up" icon={<ArrowUp size={13} />} size="sm" onClick={() => void browse(result.parent)} />
        )}
        <div className="flex-1 overflow-hidden text-ellipsis whitespace-nowrap rounded-sm border border-border bg-input px-2.5 py-1.5 font-mono text-xs text-fg">
          {result?.path ?? currentPath}
        </div>
      </div>

      {/* Directory list */}
      <div className="min-h-[180px] overflow-y-auto rounded-md border border-border">
        {loading ? (
          <div className="flex items-center justify-center gap-2 p-6 text-sm text-muted">
            <Spinner size={14} /> Loading…
          </div>
        ) : error ? (
          <div className="p-4 text-sm text-danger">{error}</div>
        ) : result && result.dirs.length === 0 ? (
          <div className="p-6 text-center text-sm text-muted">No subdirectories</div>
        ) : (
          result?.dirs.map((dir) => {
            const fullDir = `${result.path}/${dir}`
            return (
              <div key={dir} onClick={() => void browse(fullDir)} className="dir-row">
                <Folder size={14} className="dir-row-icon" />
                <span className="overflow-hidden text-ellipsis whitespace-nowrap">{dir}</span>
              </div>
            )
          })
        )}
      </div>
    </Dialog>
  )
}
