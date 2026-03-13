import {
  type Component,
  type JSX,
  createContext,
  useContext,
  createSignal,
  onCleanup,
  Show,
} from "solid-js";
import type { ServerStatus } from "@/types";

interface ServerContextValue {
  connected: () => boolean;
  reconnecting: () => boolean;
  status: () => ServerStatus | null;
  token: () => string | null;
  connect: () => Promise<void>;
  disconnect: () => void;
  fetchAPI: <T>(endpoint: string, options?: RequestInit) => Promise<T>;
}

const ServerContext = createContext<ServerContextValue>();

export const ServerProvider: Component<{ children: JSX.Element }> = (props) => {
  const [connected, setConnected] = createSignal(false);
  const [reconnecting, setReconnecting] = createSignal(false);
  const [status, setStatus] = createSignal<ServerStatus | null>(null);
  const [token, setToken] = createSignal<string | null>(
    localStorage.getItem("pando_token")
  );

  let reconnectTimeout: number | null = null;
  let healthCheckInterval: number | null = null;

  const fetchAPI = async <T,>(endpoint: string, options?: RequestInit): Promise<T> => {
    const t = token();
    const headers: HeadersInit = {
      "Content-Type": "application/json",
      ...(t ? { "X-Pando-Token": t } : {}),
      ...options?.headers,
    };

    const response = await fetch(endpoint, {
      ...options,
      headers,
    });

    if (!response.ok) {
      const error = await response.json().catch(() => ({ error: "Unknown error" }));
      throw new Error(error.error || `HTTP ${response.status}`);
    }

    return response.json();
  };

  const checkHealth = async (): Promise<boolean> => {
    try {
      const t = token();
      const response = await fetch("/health", {
        headers: t ? { "X-Pando-Token": t } : {},
      });

      if (response.ok) {
        const data = await response.json();
        setStatus({
          connected: true,
          version: data.version || "unknown",
          cwd: data.cwd || "",
        });

        const newToken = response.headers.get("X-Pando-Token");
        if (newToken) {
          localStorage.setItem("pando_token", newToken);
          setToken(newToken);
        }

        return true;
      }

      if (response.status === 401) {
        const data = await response.json();
        if (data.token) {
          localStorage.setItem("pando_token", data.token);
          setToken(data.token);
          return checkHealth();
        }
      }

      return false;
    } catch {
      return false;
    }
  };

  const connect = async () => {
    setReconnecting(true);

    const isHealthy = await checkHealth();
    setConnected(isHealthy);
    setReconnecting(false);

    if (isHealthy) {
      healthCheckInterval = window.setInterval(async () => {
        const stillHealthy = await checkHealth();
        if (!stillHealthy) {
          setConnected(false);
          scheduleReconnect();
        }
      }, 30000);
    } else {
      scheduleReconnect();
    }
  };

  const scheduleReconnect = () => {
    if (reconnectTimeout) return;
    setReconnecting(true);
    reconnectTimeout = window.setTimeout(async () => {
      reconnectTimeout = null;
      await connect();
    }, 5000);
  };

  const disconnect = () => {
    if (healthCheckInterval) {
      clearInterval(healthCheckInterval);
      healthCheckInterval = null;
    }
    if (reconnectTimeout) {
      clearTimeout(reconnectTimeout);
      reconnectTimeout = null;
    }
    setConnected(false);
    setReconnecting(false);
    setStatus(null);
  };

  onCleanup(() => {
    disconnect();
  });

  return (
    <ServerContext.Provider
      value={{
        connected,
        reconnecting,
        status,
        token,
        connect,
        disconnect,
        fetchAPI,
      }}
    >
      {props.children}
    </ServerContext.Provider>
  );
};

export const useServer = () => {
  const context = useContext(ServerContext);
  if (!context) {
    throw new Error("useServer must be used within a ServerProvider");
  }
  return context;
};
