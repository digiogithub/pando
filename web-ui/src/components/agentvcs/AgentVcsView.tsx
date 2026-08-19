import { useEffect, useRef, useState, useCallback } from "react";
import { FontAwesomeIcon } from "@fortawesome/react-fontawesome";
import {
  faCodeBranch,
  faChevronRight,
  faRotateLeft,
  faFileCirclePlus,
  faFileCircleMinus,
  faFilePen,
  faFileCode,
  faClock,
  faLayerGroup,
} from "@fortawesome/free-solid-svg-icons";
import {
  useAgentVcsStore,
  type CommitSummary,
  type DiffEntry,
  type SessionInfo,
} from "@/stores/agentVcsStore";
import LoadingSpinner from "@/components/shared/LoadingSpinner";
import EmptyState from "@/components/shared/EmptyState";
import ConfirmDialog from "@/components/shared/ConfirmDialog";
import AgentVcsDiffViewer from "./AgentVcsDiffViewer";

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function formatDate(dateStr: string): string {
  const d = new Date(dateStr);
  const now = new Date();
  const diff = now.getTime() - d.getTime();
  if (diff < 60_000) return "just now";
  if (diff < 3600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86400_000) return `${Math.floor(diff / 3600_000)}h ago`;
  return d.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function diffIcon(type: string) {
  switch (type) {
    case "added":
      return faFileCirclePlus;
    case "deleted":
      return faFileCircleMinus;
    default:
      return faFilePen;
  }
}

function diffColor(type: string) {
  switch (type) {
    case "added":
      return "#a6e3a1";
    case "deleted":
      return "#f38ba8";
    default:
      return "#fab387";
  }
}

function diffLabel(type: string) {
  switch (type) {
    case "added":
      return "A";
    case "deleted":
      return "D";
    default:
      return "M";
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
  } = useAgentVcsStore();

  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const [viewingDiff, setViewingDiff] = useState<DiffEntry | null>(null);
  const [confirmRevert, setConfirmRevert] = useState<string | null>(null);
  const [confirmFileRevert, setConfirmFileRevert] = useState<{
    commitId: string;
    file: string;
  } | null>(null);
  const [selectedFiles, setSelectedFiles] = useState<Set<string>>(new Set());

  useEffect(() => {
    fetchSessions();
    intervalRef.current = setInterval(fetchSessions, 30_000);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [fetchSessions]);

  const handleCommitClick = useCallback(
    (commitId: string) => {
      fetchCommitDetail(commitId);
      setSelectedFiles(new Set());
    },
    [fetchCommitDetail],
  );

  const toggleFileSelection = useCallback((path: string) => {
    setSelectedFiles((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  }, []);

  const handleRevertSelected = useCallback(async () => {
    if (!selectedCommit || selectedFiles.size === 0) return;
    await revertFiles(selectedCommit.commit.id, Array.from(selectedFiles));
    setSelectedFiles(new Set());
  }, [selectedCommit, selectedFiles, revertFiles]);

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        background: "var(--bg)",
      }}
    >
      {/* Header */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 10,
          padding: "1rem 1.5rem",
          borderBottom: "1px solid var(--border)",
          flexShrink: 0,
        }}
      >
        <FontAwesomeIcon
          icon={faCodeBranch}
          style={{ fontSize: 16, color: "var(--primary)" }}
        />
        <h2
          style={{
            fontSize: 16,
            fontWeight: 700,
            color: "var(--fg)",
            margin: 0,
          }}
        >
          Agent VCS
          <span
            style={{
              fontSize: 13,
              fontWeight: 400,
              color: "var(--fg-muted)",
              marginLeft: 8,
            }}
          >
            Version Control
          </span>
        </h2>
      </div>

      {/* Main content */}
      <div style={{ flex: 1, display: "flex", overflow: "hidden" }}>
        {/* Sessions panel */}
        <SessionsPanel
          sessions={sessions}
          selectedId={selectedSessionId}
          loading={loading}
          onSelect={selectSession}
        />

        {/* Commits timeline */}
        <CommitsTimeline
          commits={commits}
          selectedCommitId={selectedCommit?.commit.id ?? null}
          loading={loading && !!selectedSessionId}
          onSelect={handleCommitClick}
        />

        {/* Detail panel */}
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
          onConfirm={async () => {
            await revertToCommit(confirmRevert);
            setConfirmRevert(null);
          }}
          onCancel={() => setConfirmRevert(null)}
        />
      )}

      {/* Confirm revert single file */}
      {confirmFileRevert && (
        <ConfirmDialog
          title="Revert file"
          message={`Restore "${confirmFileRevert.file}" to its state in this commit?`}
          onConfirm={async () => {
            await revertFiles(confirmFileRevert.commitId, [
              confirmFileRevert.file,
            ]);
            setConfirmFileRevert(null);
          }}
          onCancel={() => setConfirmFileRevert(null)}
        />
      )}
    </div>
  );
}

/* ── Sessions Panel ────────────────────────────────────────── */

function SessionsPanel({
  sessions,
  selectedId,
  loading,
  onSelect,
}: {
  sessions: SessionInfo[];
  selectedId: string | null;
  loading: boolean;
  onSelect: (id: string) => void;
}) {
  return (
    <div
      style={{
        width: 240,
        minWidth: 200,
        borderRight: "1px solid var(--border)",
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
      }}
    >
      <div
        style={{
          padding: "10px 14px",
          fontSize: 11,
          fontWeight: 700,
          color: "var(--fg-muted)",
          textTransform: "uppercase",
          letterSpacing: "0.05em",
          borderBottom: "1px solid var(--border)",
        }}
      >
        <FontAwesomeIcon
          icon={faLayerGroup}
          style={{ marginRight: 6, fontSize: 10 }}
        />
        Sessions ({sessions.length})
      </div>
      <div style={{ flex: 1, overflowY: "auto" }}>
        {loading && sessions.length === 0 ? (
          <div
            style={{ display: "flex", justifyContent: "center", padding: 24 }}
          >
            <LoadingSpinner size={20} />
          </div>
        ) : sessions.length === 0 ? (
          <div style={{ padding: 16, fontSize: 12, color: "var(--fg-dim)" }}>
            No sessions with commits yet.
          </div>
        ) : (
          sessions.map((s) => (
            <button
              key={s.session_id}
              onClick={() => onSelect(s.session_id)}
              style={{
                display: "flex",
                alignItems: "center",
                gap: 8,
                width: "100%",
                padding: "8px 14px",
                background:
                  selectedId === s.session_id
                    ? "var(--hover-bg, rgba(137,180,250,0.1))"
                    : "transparent",
                border: "none",
                borderBottom: "1px solid var(--border)",
                borderLeft:
                  selectedId === s.session_id
                    ? "3px solid var(--primary)"
                    : "3px solid transparent",
                cursor: "pointer",
                color: "var(--fg)",
                fontSize: 12,
                fontFamily: "'JetBrains Mono', monospace",
                textAlign: "left",
              }}
            >
              <FontAwesomeIcon
                icon={faChevronRight}
                style={{
                  fontSize: 9,
                  color:
                    selectedId === s.session_id
                      ? "var(--primary)"
                      : "var(--fg-dim)",
                  flexShrink: 0,
                }}
              />
              <div style={{ overflow: "hidden", flex: 1 }}>
                <div
                  style={{
                    whiteSpace: "nowrap",
                    overflow: "hidden",
                    textOverflow: "ellipsis",
                    fontWeight: selectedId === s.session_id ? 600 : 400,
                  }}
                >
                  {s.session_id.slice(0, 8)}...
                </div>
                <div
                  style={{ fontSize: 10, color: "var(--fg-dim)", marginTop: 2 }}
                >
                  {s.commit_count} commit{s.commit_count !== 1 ? "s" : ""}
                </div>
              </div>
            </button>
          ))
        )}
      </div>
    </div>
  );
}

/* ── Commits Timeline ──────────────────────────────────────── */

function CommitsTimeline({
  commits,
  selectedCommitId,
  loading,
  onSelect,
}: {
  commits: CommitSummary[];
  selectedCommitId: string | null;
  loading: boolean;
  onSelect: (id: string) => void;
}) {
  // Show newest first
  const sorted = [...commits].reverse();

  return (
    <div
      style={{
        width: 320,
        minWidth: 260,
        borderRight: "1px solid var(--border)",
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
      }}
    >
      <div
        style={{
          padding: "10px 14px",
          fontSize: 11,
          fontWeight: 700,
          color: "var(--fg-muted)",
          textTransform: "uppercase",
          letterSpacing: "0.05em",
          borderBottom: "1px solid var(--border)",
        }}
      >
        <FontAwesomeIcon
          icon={faClock}
          style={{ marginRight: 6, fontSize: 10 }}
        />
        Commit Log ({commits.length})
      </div>
      <div style={{ flex: 1, overflowY: "auto" }}>
        {loading ? (
          <div
            style={{ display: "flex", justifyContent: "center", padding: 24 }}
          >
            <LoadingSpinner size={20} />
          </div>
        ) : sorted.length === 0 ? (
          <EmptyState
            title="No commits"
            description="Select a session to view its commit history."
          />
        ) : (
          sorted.map((c, i) => {
            const isSelected = selectedCommitId === c.id;
            const isFirst = i === 0;
            return (
              <button
                key={c.id}
                onClick={() => onSelect(c.id)}
                style={{
                  display: "flex",
                  gap: 10,
                  width: "100%",
                  padding: "10px 14px",
                  background: isSelected
                    ? "var(--hover-bg, rgba(137,180,250,0.1))"
                    : "transparent",
                  border: "none",
                  borderBottom: "1px solid var(--border)",
                  cursor: "pointer",
                  color: "var(--fg)",
                  textAlign: "left",
                  fontFamily: "inherit",
                }}
              >
                {/* Timeline dot + line */}
                <div
                  style={{
                    display: "flex",
                    flexDirection: "column",
                    alignItems: "center",
                    width: 14,
                    flexShrink: 0,
                    paddingTop: 4,
                  }}
                >
                  <div
                    style={{
                      width: 10,
                      height: 10,
                      borderRadius: "50%",
                      background: isFirst
                        ? "var(--primary)"
                        : c.parent_id
                          ? "var(--fg-dim)"
                          : "#a6e3a1",
                      border: isSelected
                        ? "2px solid var(--primary)"
                        : "2px solid transparent",
                      flexShrink: 0,
                    }}
                  />
                  {i < sorted.length - 1 && (
                    <div
                      style={{
                        width: 2,
                        flex: 1,
                        background: "var(--border)",
                        marginTop: 4,
                      }}
                    />
                  )}
                </div>

                {/* Content */}
                <div style={{ flex: 1, overflow: "hidden" }}>
                  <div
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: 6,
                      marginBottom: 2,
                    }}
                  >
                    <span
                      style={{
                        fontSize: 12,
                        fontWeight: 600,
                        fontFamily: "'JetBrains Mono', monospace",
                        color: isSelected ? "var(--primary)" : "var(--fg)",
                      }}
                    >
                      {c.short_id}
                    </span>
                    {!c.parent_id && (
                      <span
                        style={{
                          fontSize: 9,
                          padding: "1px 5px",
                          borderRadius: 3,
                          background: "rgba(166,227,161,0.15)",
                          color: "#a6e3a1",
                          fontWeight: 600,
                        }}
                      >
                        BASELINE
                      </span>
                    )}
                    {isFirst && c.parent_id && (
                      <span
                        style={{
                          fontSize: 9,
                          padding: "1px 5px",
                          borderRadius: 3,
                          background: "rgba(137,180,250,0.15)",
                          color: "var(--primary)",
                          fontWeight: 600,
                        }}
                      >
                        HEAD
                      </span>
                    )}
                  </div>
                  <div
                    style={{
                      fontSize: 11,
                      color: "var(--fg-muted)",
                      whiteSpace: "nowrap",
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                      marginBottom: 3,
                    }}
                    title={c.description}
                  >
                    {c.description || "No description"}
                  </div>
                  <div
                    style={{
                      display: "flex",
                      gap: 10,
                      fontSize: 10,
                      color: "var(--fg-dim)",
                    }}
                  >
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
            );
          })
        )}
      </div>
    </div>
  );
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
      id: string;
      short_id: string;
      name: string;
      session_id: string;
      parent_id: string;
      type: string;
      created_at: string;
      size: number;
      files_count: number;
    };
    diff: DiffEntry[];
  } | null;
  diff: DiffEntry[];
  loading: boolean;
  selectedFiles: Set<string>;
  onToggleFile: (path: string) => void;
  onViewDiff: (entry: DiffEntry) => void;
  onRevertAll: () => void;
  onRevertFile: (path: string) => void;
  onRevertSelected: () => void;
}) {
  if (loading) {
    return (
      <div
        style={{
          flex: 1,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <LoadingSpinner size={24} />
      </div>
    );
  }

  if (!commit) {
    return (
      <div
        style={{
          flex: 1,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <EmptyState
          title="Select a commit"
          description="Click on a commit in the timeline to view its details and file changes."
        />
      </div>
    );
  }

  const c = commit.commit;

  return (
    <div
      style={{
        flex: 1,
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
      }}
    >
      {/* Commit info header */}
      <div
        style={{
          padding: "12px 16px",
          borderBottom: "1px solid var(--border)",
          background: "var(--surface, var(--bg))",
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            marginBottom: 8,
          }}
        >
          <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <FontAwesomeIcon
              icon={faCodeBranch}
              style={{ fontSize: 13, color: "var(--primary)" }}
            />
            <span
              style={{
                fontSize: 14,
                fontWeight: 700,
                fontFamily: "'JetBrains Mono', monospace",
                color: "var(--primary)",
              }}
            >
              {c.short_id}
            </span>
            <span
              style={{
                fontSize: 10,
                padding: "2px 6px",
                borderRadius: 3,
                background:
                  c.type === "start"
                    ? "rgba(166,227,161,0.15)"
                    : "rgba(250,179,135,0.15)",
                color: c.type === "start" ? "#a6e3a1" : "#fab387",
                fontWeight: 600,
              }}
            >
              {c.type.toUpperCase()}
            </span>
          </div>

          <button
            onClick={onRevertAll}
            style={{
              display: "flex",
              alignItems: "center",
              gap: 6,
              padding: "5px 12px",
              borderRadius: "var(--radius-sm, 4px)",
              border: "1px solid var(--error, #f38ba8)",
              background: "transparent",
              color: "var(--error, #f38ba8)",
              fontSize: 11,
              fontWeight: 600,
              cursor: "pointer",
              fontFamily: "inherit",
            }}
            onMouseEnter={(e) => {
              e.currentTarget.style.background = "var(--error, #f38ba8)";
              e.currentTarget.style.color = "#fff";
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.background = "transparent";
              e.currentTarget.style.color = "var(--error, #f38ba8)";
            }}
          >
            <FontAwesomeIcon icon={faRotateLeft} style={{ fontSize: 10 }} />
            Revert All
          </button>
        </div>

        <div
          style={{ fontSize: 12, color: "var(--fg-muted)", marginBottom: 4 }}
        >
          {c.name}
        </div>
        <div
          style={{
            display: "flex",
            gap: 16,
            fontSize: 11,
            color: "var(--fg-dim)",
          }}
        >
          <span>{formatDate(c.created_at)}</span>
          <span>{c.files_count} tracked files</span>
          <span>{formatSize(c.size)}</span>
          {c.parent_id && (
            <span style={{ fontFamily: "'JetBrains Mono', monospace" }}>
              parent: {c.parent_id.slice(0, 12)}
            </span>
          )}
        </div>
      </div>

      {/* Changed files header */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          padding: "8px 16px",
          borderBottom: "1px solid var(--border)",
          fontSize: 11,
          fontWeight: 700,
          color: "var(--fg-muted)",
          textTransform: "uppercase",
          letterSpacing: "0.05em",
        }}
      >
        <span>
          <FontAwesomeIcon
            icon={faFileCode}
            style={{ marginRight: 6, fontSize: 10 }}
          />
          Changed Files ({diff.length})
        </span>
        {selectedFiles.size > 0 && (
          <button
            onClick={onRevertSelected}
            style={{
              display: "flex",
              alignItems: "center",
              gap: 4,
              padding: "3px 8px",
              borderRadius: 3,
              border: "1px solid var(--warning, #fab387)",
              background: "transparent",
              color: "var(--warning, #fab387)",
              fontSize: 10,
              fontWeight: 600,
              cursor: "pointer",
              textTransform: "none",
              fontFamily: "inherit",
            }}
          >
            <FontAwesomeIcon icon={faRotateLeft} style={{ fontSize: 9 }} />
            Revert {selectedFiles.size} selected
          </button>
        )}
      </div>

      {/* File list */}
      <div style={{ flex: 1, overflowY: "auto" }}>
        {diff.length === 0 ? (
          <div
            style={{
              padding: 24,
              textAlign: "center",
              fontSize: 12,
              color: "var(--fg-dim)",
            }}
          >
            {c.type === "baseline" || c.type === "start"
              ? "Session baseline — reference state captured at session start, not a change set."
              : "No file changes in this commit."}
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
  );
}

/* ── File Row ──────────────────────────────────────────────── */

function FileRow({
  entry,
  selected,
  onToggle,
  onViewDiff,
  onRevert,
}: {
  entry: DiffEntry;
  selected: boolean;
  onToggle: () => void;
  onViewDiff: () => void;
  onRevert: () => void;
}) {
  const fileName = entry.path.split("/").pop() ?? entry.path;
  const dirPath = entry.path.includes("/")
    ? entry.path.slice(0, entry.path.lastIndexOf("/"))
    : "";

  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: 8,
        padding: "6px 16px",
        borderBottom: "1px solid var(--border)",
        fontSize: 12,
        background: selected ? "rgba(137,180,250,0.06)" : "transparent",
      }}
    >
      {/* Checkbox */}
      <input
        type="checkbox"
        checked={selected}
        onChange={onToggle}
        style={{
          accentColor: "var(--primary)",
          cursor: "pointer",
          flexShrink: 0,
        }}
      />

      {/* Type badge */}
      <span
        style={{
          display: "inline-flex",
          alignItems: "center",
          justifyContent: "center",
          width: 20,
          height: 20,
          borderRadius: 3,
          fontSize: 10,
          fontWeight: 700,
          color: diffColor(entry.type),
          background: `${diffColor(entry.type)}15`,
          flexShrink: 0,
        }}
        title={entry.type}
      >
        {diffLabel(entry.type)}
      </span>

      {/* File icon + path */}
      <div
        style={{
          flex: 1,
          overflow: "hidden",
          display: "flex",
          alignItems: "center",
          gap: 6,
          cursor: "pointer",
        }}
        onClick={onViewDiff}
        title={`View diff: ${entry.path}`}
      >
        <FontAwesomeIcon
          icon={diffIcon(entry.type)}
          style={{ fontSize: 11, color: diffColor(entry.type), flexShrink: 0 }}
        />
        <span
          style={{
            fontFamily: "'JetBrains Mono', monospace",
            fontWeight: 600,
            color: "var(--fg)",
            whiteSpace: "nowrap",
          }}
        >
          {fileName}
        </span>
        {dirPath && (
          <span
            style={{
              fontSize: 10,
              color: "var(--fg-dim)",
              whiteSpace: "nowrap",
              overflow: "hidden",
              textOverflow: "ellipsis",
            }}
          >
            {dirPath}
          </span>
        )}
      </div>

      {/* Size info */}
      <span style={{ fontSize: 10, color: "var(--fg-dim)", flexShrink: 0 }}>
        {entry.new_size
          ? formatSize(entry.new_size)
          : entry.old_size
            ? formatSize(entry.old_size)
            : ""}
      </span>

      {/* Revert button */}
      <button
        onClick={onRevert}
        title={`Revert ${fileName}`}
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          width: 24,
          height: 24,
          borderRadius: 3,
          border: "1px solid var(--border)",
          background: "transparent",
          color: "var(--fg-dim)",
          cursor: "pointer",
          fontSize: 10,
          flexShrink: 0,
        }}
        onMouseEnter={(e) => {
          e.currentTarget.style.borderColor = "var(--error, #f38ba8)";
          e.currentTarget.style.color = "var(--error, #f38ba8)";
        }}
        onMouseLeave={(e) => {
          e.currentTarget.style.borderColor = "var(--border)";
          e.currentTarget.style.color = "var(--fg-dim)";
        }}
      >
        <FontAwesomeIcon icon={faRotateLeft} />
      </button>
    </div>
  );
}
