import api, { getBaseURL } from '@/services/api'

export interface PtyReadyInfo {
  sessionId: string
  shell?: string
  dir?: string
  reattach: boolean
}

export interface PtyHandlers {
  onData: (chunk: Uint8Array) => void
  onReady: (info: PtyReadyInfo) => void
  onExit: (code: number) => void
  onClose: (err?: Error) => void
}

export interface PtyConnection {
  send: (data: string) => void
  resize: (cols: number, rows: number) => void
  /** Close the socket but leave the shell running, so a reload can reattach. */
  detach: () => void
  /** Kill the shell server-side and close the socket. */
  terminate: () => void
}

/**
 * Live connections keyed by terminal tab id. The pane component owns the
 * connection, but closing a tab has to kill the shell from the store, which
 * must not reach into components — so they meet here.
 */
const connections = new Map<string, PtyConnection>()

export function registerPtyConnection(tabId: string, connection: PtyConnection): void {
  connections.set(tabId, connection)
}

export function unregisterPtyConnection(tabId: string): void {
  connections.delete(tabId)
}

/** Sends raw input to a tab's shell, as if typed. */
export function sendPtyInput(tabId: string, data: string): void {
  connections.get(tabId)?.send(data)
}

/** Kills the shell behind a tab. Used when the tab is closed for good. */
export function terminatePtyConnection(tabId: string): void {
  const connection = connections.get(tabId)
  if (!connection) return
  connection.terminate()
  connections.delete(tabId)
}

interface PtyControlMessage {
  type: string
  session_id?: string
  shell?: string
  dir?: string
  reattach?: boolean
  code?: number
  message?: string
}

/**
 * Builds the websocket URL for the PTY endpoint. The token travels as a query
 * param because the browser WebSocket API cannot set custom headers; the server
 * accepts `?token=` in authMiddleware for exactly this reason.
 */
function buildPtyURL(params: { sessionId?: string; cols: number; rows: number }): string {
  const base = getBaseURL()
  const httpOrigin = base || (typeof window !== 'undefined' ? window.location.origin : '')
  const url = new URL('/api/v1/terminal/pty', httpOrigin)
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'

  url.searchParams.set('cols', String(params.cols))
  url.searchParams.set('rows', String(params.rows))
  if (params.sessionId) {
    url.searchParams.set('session_id', params.sessionId)
  }
  const token = api.getToken()
  if (token) {
    url.searchParams.set('token', token)
  }
  return url.toString()
}

/**
 * Opens an interactive shell over a websocket. Raw terminal I/O travels as
 * binary frames; JSON text frames carry control (resize/exit/ready).
 */
export function connectPty(
  params: { sessionId?: string; cols: number; rows: number },
  handlers: PtyHandlers,
): PtyConnection {
  const socket = new WebSocket(buildPtyURL(params))
  socket.binaryType = 'arraybuffer'

  let closedByUs = false

  socket.onmessage = (event) => {
    if (typeof event.data === 'string') {
      let msg: PtyControlMessage
      try {
        msg = JSON.parse(event.data) as PtyControlMessage
      } catch {
        return
      }
      if (msg.type === 'ready') {
        handlers.onReady({
          sessionId: msg.session_id ?? '',
          shell: msg.shell,
          dir: msg.dir,
          reattach: Boolean(msg.reattach),
        })
      } else if (msg.type === 'exit') {
        handlers.onExit(msg.code ?? 0)
      }
      return
    }
    handlers.onData(new Uint8Array(event.data as ArrayBuffer))
  }

  socket.onerror = () => {
    if (!closedByUs) {
      handlers.onClose(new Error('terminal connection failed'))
    }
  }

  socket.onclose = () => {
    if (!closedByUs) {
      handlers.onClose()
    }
  }

  const sendControl = (payload: Record<string, unknown>) => {
    if (socket.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify(payload))
    }
  }

  return {
    send: (data: string) => {
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(new TextEncoder().encode(data))
      }
    },
    resize: (cols: number, rows: number) => sendControl({ type: 'resize', cols, rows }),
    detach: () => {
      closedByUs = true
      socket.close()
    },
    terminate: () => {
      closedByUs = true
      sendControl({ type: 'close' })
      socket.close()
    },
  }
}
