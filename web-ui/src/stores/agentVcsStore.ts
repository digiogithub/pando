import { create } from "zustand";
import api from "@/services/api";
import { useToastStore } from "@/stores/toastStore";

export type DiffType = "added" | "modified" | "deleted";

export interface CommitSummary {
  id: string;
  short_id: string;
  parent_id: string;
  session_id: string;
  description: string;
  file_count: number;
  total_size: number;
  changed_files: number;
  changed_total_size: number;
  is_baseline: boolean;
  created_at: string;
}

export interface DiffEntry {
  path: string;
  type: DiffType;
  old_hash?: string;
  new_hash?: string;
  old_size?: number;
  new_size?: number;
}

export interface CommitDetail {
  commit: {
    id: string;
    short_id: string;
    name: string;
    session_id: string;
    parent_id: string;
    type: string;
    status: string;
    created_at: string;
    size: number;
    files_count: number;
  };
  diff: DiffEntry[];
}

export interface SessionInfo {
  session_id: string;
  commit_count: number;
}

interface AgentVcsStore {
  sessions: SessionInfo[];
  commits: CommitSummary[];
  selectedSessionId: string | null;
  selectedCommit: CommitDetail | null;
  selectedCommitDiff: DiffEntry[];
  loading: boolean;
  loadingCommit: boolean;

  fetchSessions: () => Promise<void>;
  fetchCommits: (sessionId: string) => Promise<void>;
  fetchCommitDetail: (commitId: string) => Promise<void>;
  fetchBlobContent: (hash: string) => Promise<string>;
  revertToCommit: (commitId: string) => Promise<void>;
  revertFiles: (commitId: string, files: string[]) => Promise<string[]>;
  selectSession: (sessionId: string) => void;
  clearSelection: () => void;
}

export const useAgentVcsStore = create<AgentVcsStore>((set, get) => ({
  sessions: [],
  commits: [],
  selectedSessionId: null,
  selectedCommit: null,
  selectedCommitDiff: [],
  loading: false,
  loadingCommit: false,

  fetchSessions: async () => {
    set({ loading: true });
    try {
      const data = await api.get<{ sessions: SessionInfo[] }>(
        "/api/v1/agentvcs/sessions",
      );
      set({ sessions: data.sessions ?? [], loading: false });
    } catch {
      set({ sessions: [], loading: false });
    }
  },

  fetchCommits: async (sessionId: string) => {
    set({ loading: true, selectedSessionId: sessionId });
    try {
      const data = await api.get<{ commits: CommitSummary[] }>(
        `/api/v1/agentvcs/sessions/${sessionId}/log`,
      );
      set({ commits: data.commits ?? [], loading: false });
    } catch {
      set({ commits: [], loading: false });
    }
  },

  fetchCommitDetail: async (commitId: string) => {
    set({ loadingCommit: true });
    try {
      const data = await api.get<CommitDetail>(
        `/api/v1/agentvcs/commits/${commitId}`,
      );
      set({
        selectedCommit: data,
        selectedCommitDiff: data.diff ?? [],
        loadingCommit: false,
      });
    } catch {
      set({
        selectedCommit: null,
        selectedCommitDiff: [],
        loadingCommit: false,
      });
    }
  },

  fetchBlobContent: async (hash: string) => {
    const res = await api.get<string>(`/api/v1/agentvcs/blobs/${hash}`);
    return res;
  },

  revertToCommit: async (commitId: string) => {
    try {
      await api.post("/api/v1/agentvcs/commits/" + commitId + "/revert", {});
      useToastStore
        .getState()
        .addToast("Reverted to commit " + commitId.slice(0, 12), "success");
      // Refresh commits
      const sid = get().selectedSessionId;
      if (sid) await get().fetchCommits(sid);
    } catch (e) {
      useToastStore
        .getState()
        .addToast(e instanceof Error ? e.message : "Revert failed", "error");
    }
  },

  revertFiles: async (commitId: string, files: string[]) => {
    try {
      const data = await api.post<{ restored: string[] }>(
        "/api/v1/agentvcs/commits/" + commitId + "/revert-files",
        { files },
      );
      const restored = data.restored ?? [];
      useToastStore
        .getState()
        .addToast(`Restored ${restored.length} file(s)`, "success");
      return restored;
    } catch (e) {
      useToastStore
        .getState()
        .addToast(
          e instanceof Error ? e.message : "Revert files failed",
          "error",
        );
      return [];
    }
  },

  selectSession: (sessionId: string) => {
    set({
      selectedSessionId: sessionId,
      selectedCommit: null,
      selectedCommitDiff: [],
    });
    get().fetchCommits(sessionId);
  },

  clearSelection: () => {
    set({
      selectedCommit: null,
      selectedCommitDiff: [],
      selectedSessionId: null,
      commits: [],
    });
  },
}));
