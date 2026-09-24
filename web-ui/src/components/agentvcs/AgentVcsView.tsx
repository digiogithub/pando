import clsx from 'clsx'
import { useEffect, useRef, useState, useCallback } from 'react'
import {
  useAgentVcsStore,
  type CommitSummary,
  type DiffEntry,
  type SessionInfo,
} from '@pando/client/stores/agentVcsStore'
import { Badge, Button, Checkbox, EmptyState, IconButton, Spinner } from '@/components/ui'
import {
  ChevronRight,
  Clock,
  FileCode,
  GitBranch,
  Layers,
  RotateCcw,
} from '@/components/ui/icons'
import ConfirmDialog from '@/components/shared/ConfirmDialog'
import AgentVcsDiffViewer from './AgentVcsDiffViewer'
import '@/styles/agentvcs.css'

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function formatDate(dateStr: string): string {
  const d = new Date(dateStr)
  const now = new Date()
  const diff = now.getTime() - d.getTime()
  if (diff < 60_000) return 'just now'
  if (diff < 3600_000) return `${Math.floor(diff / 60_000)}m ago`
  if (diff < 86400_000) return `${Math.floor(diff / 3600_000)}h ago`
  return d.toLocaleDateString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function diffLabel(type: string): string {
  switch (type) {
    case 'added': return 'A'
    case 'deleted': return 'D'
    default: return 'M'
  }
}

function diffTone(type: string): 'success' | 'danger' | 'warning' {
  switch (type) {
    case 'added': return 'success'
    case 'deleted': return 'danger'
    default: return 'warning'
  }
}

export default function AgentVcsView() {
  const {
    sessions,
    commits,
    selectedSessionId,
    selectedCommit,
    selectedCommitDiff,
    loading,
    loadingCommit,
    fetchSessions,
    selectSession,
    fetchCommitDetail,
    revertToCommit,
    revertFiles,
  } = useAgentVcsStore()

  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const [viewingDiff, setViewingDiff] = useState<DiffEntry | null>(null)
  const [confirmRevert, setConfirmRevert] = useState<string | null>(null)
  const [confirmFileRevert, setConfirmFileRevert] = useState<{
    commitId: string
    file: string
  } | null>(null)
  const [selectedFiles, setSelectedFiles] = useState<Set<string>>(new Set())

  useEffect(() => {
    fetchSessions()
    intervalRef.current = setInterval(fetchSessions, 30_000)
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current)
    }
  }, [fetchSessions])

  const handleCommitClick = useCallback(
    (commitId: string) => {
      fetchCommitDetail(commitId)
      setSelectedFiles(new Set())
    },
    [fetchCommitDetail],
  )

  const toggleFileSelection = useCallback((path: string) => {
    setSelectedFiles((prev) => {
      const next = new Set(prev)
      if (next.has(path)) next.delete(path)
      else next.add(path)
      return next
    })
  }, [])

  const handleRevertSelected = useCallback(async () => {
    if (!selectedCommit || selectedFiles.size === 0) return
    await revertFiles(selectedCommit.commit.id, Array.from(selectedFiles))
    setSelectedFiles(new Set())
  }, [selectedCommit, selectedFiles, revertFiles])

  return (
    <div className="agentvcs-shell">
      {/* Header */}
      <div className="agentvcs-header">
        <GitBranch size={17} />
        <h2 className="agentvcs-header-title">
          Agent VCS
          <span className="agentvcs-header-subtitle">Version Control</span>
        </h2>
      </div>

      {/* Main content */}
      <div className="agentvcs-body">
        <SessionsPanel
          sessions={sessions}
          selectedId={selectedSessionId}
          loading={loading}
          onSelect={selectSession}
        />

        <CommitsTimeline
          commits={commits}
          selectedCommitId={selectedCommit?.commit.id ?? null}
          loading={loading && !!selectedSessionId}
          onSelect={handleCommitClick}
        />

        <DetailPanel
          commit={selectedCommit}
          diff={selectedCommitDiff}
          loading={loadingCommit}
          selectedFiles={selectedFiles}
          onToggleFile={toggleFileSelection}
          onViewDiff={setViewingDiff}
          onRevertAll={() =>
            selectedCommit && setConfirmRevert(selectedCommit.commit.id)
          }
          onRevertFile={(path) =>
            selectedCommit &&
            setConfirmFileRevert({
              commitId: selectedCommit.commit.id,
              file: path,
            })
          }
          onRevertSelected={handleRevertSelected}
        />
      </div>

      {/* Diff viewer overlay */}
      {viewingDiff && selectedCommit && (
        <AgentVcsDiffViewer
          entry={viewingDiff}
          commitId={selectedCommit.commit.id}
          onClose={() => setViewingDiff(null)}
        />
      )}

      {/* Confirm revert all */}
      {confirmRevert && (
        <ConfirmDialog
          title="Revert to commit"
          message="This will restore all files to the state of this commit. A safety backup will be created automatically. Continue?"
          dangerous
          onConfirm={async () => {
            await revertToCommit(confirmRevert)
            setConfirmRevert(null)
          }}
          onCancel={() => setConfirmRevert(null)}
        />
      )}

      {/* Confirm revert single file */}
      {confirmFileRevert && (
        <ConfirmDialog
          title="Revert file"
          message={`Restore "${confirmFileRevert.file}" to its state in this commit?`}
          dangerous
          onConfirm={async () => {
            await revertFiles(confirmFileRevert.commitId, [confirmFileRevert.file])
            setConfirmFileRevert(null)
          }}
          onCancel={() => setConfirmFileRevert(null)}
        />
      )}
    </div>
  )
}

/* ── Sessions Panel ────────────────────────────────────────── */

function SessionsPanel({
  sessions,
  selectedId,
  loading,
  onSelect,
}: {
  sessions: SessionInfo[]
  selectedId: string | null
  loading: boolean
  onSelect: (id: string) => void
}) {
  return (
    <div className="agentvcs-panel agentvcs-panel--sessions">
      <div className="agentvcs-panel-header">
        <Layers size={11} />
        Sessions ({sessions.length})
      </div>
      <div className="agentvcs-panel-list">
        {loading && sessions.length === 0 ? (
          <div className="agentvcs-panel-loading"><Spinner size={20} /></div>
        ) : sessions.length === 0 ? (
          <div className="agentvcs-files-empty">No sessions with commits yet.</div>
        ) : (
          sessions.map((s) => {
            const active = selectedId === s.session_id
            return (
              <button
                key={s.session_id}
                type="button"
                onClick={() => onSelect(s.session_id)}
                className={clsx('agentvcs-session-row', active && 'agentvcs-session-row--active')}
              >
                <ChevronRight size={11} />
                <div style={{ overflow: 'hidden', flex: 1 }}>
                  <div className="agentvcs-session-id" style={{ fontWeight: active ? 600 : 400 }}>
                    {s.session_id.slice(0, 8)}...
                  </div>
                  <div className="agentvcs-session-count">
                    {s.commit_count} commit{s.commit_count !== 1 ? 's' : ''}
                  </div>
                </div>
              </button>
            )
          })
        )}
      </div>
    </div>
  )
}

/* ── Commits Timeline ──────────────────────────────────────── */

function CommitsTimeline({
  commits,
  selectedCommitId,
  loading,
  onSelect,
}: {
  commits: CommitSummary[]
  selectedCommitId: string | null
  loading: boolean
  onSelect: (id: string) => void
}) {
  // Show newest first
  const sorted = [...commits].reverse()

  return (
    <div className="agentvcs-panel agentvcs-panel--timeline">
      <div className="agentvcs-panel-header">
        <Clock size={11} />
        Commit Log ({commits.length})
      </div>
      <div className="agentvcs-panel-list">
        {loading ? (
          <div className="agentvcs-panel-loading"><Spinner size={20} /></div>
        ) : sorted.length === 0 ? (
          <EmptyState title="No commits" description="Select a session to view its commit history." />
        ) : (
          sorted.map((c, i) => {
            const isSelected = selectedCommitId === c.id
            const isFirst = i === 0
            return (
              <button
                key={c.id}
                type="button"
                onClick={() => onSelect(c.id)}
                className={clsx('agentvcs-commit-row', isSelected && 'agentvcs-commit-row--active')}
              >
                {/* Timeline dot + line */}
                <div className="agentvcs-commit-dot-col">
                  <div
                    className={clsx(
                      'agentvcs-commit-dot',
                      isFirst && 'agentvcs-commit-dot--head',
                      !c.parent_id && 'agentvcs-commit-dot--baseline',
                      isSelected && 'agentvcs-commit-dot--active',
                    )}
                  />
                  {i < sorted.length - 1 && <div className="agentvcs-commit-line" />}
                </div>

                {/* Content */}
                <div style={{ flex: 1, overflow: 'hidden' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 2 }}>
                    <span className="agentvcs-commit-id">{c.short_id}</span>
                    {!c.parent_id && <Badge tone="success">BASELINE</Badge>}
                    {isFirst && c.parent_id && <Badge tone="accent">HEAD</Badge>}
                  </div>
                  <div className="agentvcs-commit-desc" title={c.description}>
                    {c.description || 'No description'}
                  </div>
                  <div className="agentvcs-commit-meta">
                    <span>{formatDate(c.created_at)}</span>
                    {c.is_baseline || !c.parent_id ? (
                      <span>session start state — no changes</span>
                    ) : (
                      <>
                        <span>{c.changed_files} files changed</span>
                        <span>{formatSize(c.changed_total_size)}</span>
                      </>
                    )}
                  </div>
                </div>
              </button>
            )
          })
        )}
      </div>
    </div>
  )
}

/* ── Detail Panel ──────────────────────────────────────────── */

function DetailPanel({
  commit,
  diff,
  loading,
  selectedFiles,
  onToggleFile,
  onViewDiff,
  onRevertAll,
  onRevertFile,
  onRevertSelected,
}: {
  commit: {
    commit: {
      id: string
      short_id: string
      name: string
      session_id: string
      parent_id: string
      type: string
      created_at: string
      size: number
      files_count: number
    }
    diff: DiffEntry[]
  } | null
  diff: DiffEntry[]
  loading: boolean
  selectedFiles: Set<string>
  onToggleFile: (path: string) => void
  onViewDiff: (entry: DiffEntry) => void
  onRevertAll: () => void
  onRevertFile: (path: string) => void
  onRevertSelected: () => void
}) {
  if (loading) {
    return (
      <div className="agentvcs-detail" style={{ alignItems: 'center', justifyContent: 'center' }}>
        <Spinner size={24} />
      </div>
    )
  }

  if (!commit) {
    return (
      <div className="agentvcs-detail" style={{ alignItems: 'center', justifyContent: 'center' }}>
        <EmptyState
          icon={<GitBranch size={22} />}
          title="Select a commit"
          description="Click on a commit in the timeline to view its details and file changes."
        />
      </div>
    )
  }

  const c = commit.commit

  return (
    <div className="agentvcs-detail">
      {/* Commit info header */}
      <div className="agentvcs-detail-header">
        <div className="agentvcs-detail-top">
          <div className="agentvcs-detail-id">
            <GitBranch size={14} />
            <span className="agentvcs-detail-id-text">{c.short_id}</span>
            <Badge tone={c.type === 'start' ? 'success' : 'warning'}>{c.type.toUpperCase()}</Badge>
          </div>

          <Button size="sm" variant="danger" icon={<RotateCcw size={12} />} onClick={onRevertAll}>
            Revert All
          </Button>
        </div>

        <div className="agentvcs-detail-name">{c.name}</div>
        <div className="agentvcs-detail-meta">
          <span>{formatDate(c.created_at)}</span>
          <span>{c.files_count} tracked files</span>
          <span>{formatSize(c.size)}</span>
          {c.parent_id && <code>parent: {c.parent_id.slice(0, 12)}</code>}
        </div>
      </div>

      {/* Changed files header */}
      <div className="agentvcs-files-header">
        <span>
          <FileCode size={11} />
          Changed Files ({diff.length})
        </span>
        {selectedFiles.size > 0 && (
          <Button size="sm" variant="secondary" icon={<RotateCcw size={10} />} onClick={onRevertSelected}>
            Revert {selectedFiles.size} selected
          </Button>
        )}
      </div>

      {/* File list */}
      <div className="agentvcs-files-list">
        {diff.length === 0 ? (
          <div className="agentvcs-files-empty">
            {c.type === 'baseline' || c.type === 'start'
              ? 'Session baseline — reference state captured at session start, not a change set.'
              : 'No file changes in this commit.'}
          </div>
        ) : (
          diff.map((entry) => (
            <FileRow
              key={entry.path}
              entry={entry}
              selected={selectedFiles.has(entry.path)}
              onToggle={() => onToggleFile(entry.path)}
              onViewDiff={() => onViewDiff(entry)}
              onRevert={() => onRevertFile(entry.path)}
            />
          ))
        )}
      </div>
    </div>
  )
}

/* ── File Row ──────────────────────────────────────────────── */

function FileRow({
  entry,
  selected,
  onToggle,
  onViewDiff,
  onRevert,
}: {
  entry: DiffEntry
  selected: boolean
  onToggle: () => void
  onViewDiff: () => void
  onRevert: () => void
}) {
  const fileName = entry.path.split('/').pop() ?? entry.path
  const dirPath = entry.path.includes('/')
    ? entry.path.slice(0, entry.path.lastIndexOf('/'))
    : ''

  return (
    <div className={clsx('agentvcs-file-row', selected && 'agentvcs-file-row--selected')}>
      <Checkbox checked={selected} onCheckedChange={onToggle} aria-label={`Select ${fileName}`} />

      <Badge tone={diffTone(entry.type)} title={entry.type}>{diffLabel(entry.type)}</Badge>

      <div className="agentvcs-file-main" onClick={onViewDiff} title={`View diff: ${entry.path}`}>
        <FileCode size={13} />
        <span className="agentvcs-file-name">{fileName}</span>
        {dirPath && <span className="agentvcs-file-dir">{dirPath}</span>}
      </div>

      <span className="agentvcs-file-size">
        {entry.new_size ? formatSize(entry.new_size) : entry.old_size ? formatSize(entry.old_size) : ''}
      </span>

      <IconButton aria-label={`Revert ${fileName}`} tooltip icon={<RotateCcw size={12} />} size="sm" onClick={onRevert} />
    </div>
  )
}
