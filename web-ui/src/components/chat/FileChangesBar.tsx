import { useState, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { useFileChangesStore, type FileChange } from '@pando/client/stores/fileChangesStore'
import { ChevronRight, FileCode } from '@/components/ui/icons'
import DiffViewer from './DiffViewer'

export default function FileChangesBar() {
  const { t } = useTranslation()
  const changes = useFileChangesStore((s) => s.changes)
  const [collapsed, setCollapsed] = useState(false)
  const [viewingDiff, setViewingDiff] = useState<FileChange | null>(null)

  const fileList = useMemo(() => {
    return Object.values(changes).sort((a, b) => b.lastUpdated - a.lastUpdated)
  }, [changes])

  if (fileList.length === 0) return null

  const totalAdditions = fileList.reduce((sum, f) => sum + f.additions, 0)
  const totalRemovals = fileList.reduce((sum, f) => sum + f.removals, 0)

  return (
    <>
      <div className="chat-files">
        <button type="button" className="chat-files-head" aria-expanded={!collapsed} onClick={() => setCollapsed((v) => !v)}>
          <ChevronRight size={14} className="chat-chevron" />
          <FileCode size={14} />
          <span>{t('chat.files.changed', { count: fileList.length })}</span>
          <span className="chat-files-stats">
            {totalAdditions > 0 && <span className="chat-add">+{totalAdditions}</span>}
            {totalRemovals > 0 && <span className="chat-del">-{totalRemovals}</span>}
          </span>
        </button>

        {!collapsed && (
          <div className="chat-files-list">
            {fileList.map((fc) => (
              <FileChip key={fc.filePath} file={fc} onView={() => setViewingDiff(fc)} />
            ))}
          </div>
        )}
      </div>

      {viewingDiff && <DiffViewer file={viewingDiff} onClose={() => setViewingDiff(null)} />}
    </>
  )
}

function FileChip({ file, onView }: { file: FileChange; onView: () => void }) {
  const { t } = useTranslation()
  return (
    <button type="button" className="chat-file-chip" onClick={onView} title={t('chat.files.viewDiff', { path: file.filePath })}>
      <span className="chat-file-name">{file.fileName}</span>
      <span className="chat-file-stats">
        {file.additions > 0 && <span className="chat-add">+{file.additions}</span>}
        {file.removals > 0 && <span className="chat-del">-{file.removals}</span>}
      </span>
    </button>
  )
}
