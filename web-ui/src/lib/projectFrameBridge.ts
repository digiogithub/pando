import type { AccentPreset, ThemeId } from '@/hooks/useTheme'
import type { UIScale } from '@/components/settings/uiScale'

export type ProjectFrameNotificationLevel = 'success' | 'error' | 'warning' | 'info'

export interface ProjectFrameShortcutMessage {
  type: 'pando:shortcut'
  key: string
  ctrl: boolean
  alt: boolean
  shift: boolean
  meta: boolean
}

export type ProjectFrameChildMessage =
  | { type: 'pando:title'; title: string }
  | { type: 'pando:busy'; busy: boolean }
  | { type: 'pando:notification'; level: ProjectFrameNotificationLevel; message: string }
  | ProjectFrameShortcutMessage

export type ProjectFrameParentMessage =
  | { type: 'pando:focus' }
  | { type: 'pando:theme'; themeId: ThemeId; accent: AccentPreset | null; uiSize: UIScale }
  | { type: 'pando:language'; lang: string }

type ParentWindowLike = Pick<Window, 'addEventListener' | 'removeEventListener' | 'location'>
type ChildWindowLike = Pick<Window, 'addEventListener' | 'removeEventListener' | 'location' | 'parent'>

export interface ProjectFrameBridgeTarget {
  projectId: string
  source: MessageEventSource | null
  postMessage: (message: ProjectFrameParentMessage, targetOrigin: string) => void
}

interface ProjectFrameParentBridgeOptions {
  hostWindow?: ParentWindowLike
  getTargetBySource: (source: MessageEventSource | null) => ProjectFrameBridgeTarget | null
  getTargetByProjectId: (projectId: string) => ProjectFrameBridgeTarget | null
  onTitle: (projectId: string, title: string) => void
  onBusy: (projectId: string, busy: boolean) => void
  onNotification: (projectId: string, level: ProjectFrameNotificationLevel, message: string) => void
  onShortcut: (projectId: string, shortcut: Omit<ProjectFrameShortcutMessage, 'type'>) => void
}

interface ProjectFrameChildBridgeOptions {
  hostWindow?: ChildWindowLike
  parentWindow?: MessageEventSource | null
  onFocus: () => void
  onTheme: (payload: Extract<ProjectFrameParentMessage, { type: 'pando:theme' }>) => void
  onLanguage: (payload: Extract<ProjectFrameParentMessage, { type: 'pando:language' }>) => void
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object'
}

export function parseProjectFrameChildMessage(value: unknown): ProjectFrameChildMessage | null {
  if (!isRecord(value) || typeof value.type !== 'string') return null

  switch (value.type) {
    case 'pando:title':
      return typeof value.title === 'string'
        ? { type: value.type, title: value.title }
        : null
    case 'pando:busy':
      return typeof value.busy === 'boolean'
        ? { type: value.type, busy: value.busy }
        : null
    case 'pando:notification':
      return typeof value.message === 'string'
        && (value.level === 'success' || value.level === 'error' || value.level === 'warning' || value.level === 'info')
        ? { type: value.type, level: value.level, message: value.message }
        : null
    case 'pando:shortcut':
      return typeof value.key === 'string'
        && typeof value.ctrl === 'boolean'
        && typeof value.alt === 'boolean'
        && typeof value.shift === 'boolean'
        && typeof value.meta === 'boolean'
        ? {
            type: value.type,
            key: value.key,
            ctrl: value.ctrl,
            alt: value.alt,
            shift: value.shift,
            meta: value.meta,
          }
        : null
    default:
      return null
  }
}

export function parseProjectFrameParentMessage(value: unknown): ProjectFrameParentMessage | null {
  if (!isRecord(value) || typeof value.type !== 'string') return null

  switch (value.type) {
    case 'pando:focus':
      return { type: value.type }
    case 'pando:theme':
      return typeof value.themeId === 'string'
        && (typeof value.accent === 'string' || value.accent === null)
        && (value.uiSize === 'small' || value.uiSize === 'default' || value.uiSize === 'large')
        ? {
            type: value.type,
            themeId: value.themeId as ThemeId,
            accent: value.accent as AccentPreset | null,
            uiSize: value.uiSize,
          }
        : null
    case 'pando:language':
      return typeof value.lang === 'string'
        ? { type: value.type, lang: value.lang }
        : null
    default:
      return null
  }
}

export function createProjectFrameParentBridge({
  hostWindow = window,
  getTargetBySource,
  getTargetByProjectId,
  onTitle,
  onBusy,
  onNotification,
  onShortcut,
}: ProjectFrameParentBridgeOptions) {
  const origin = hostWindow.location.origin

  const onMessage = (event: MessageEvent) => {
    if (event.origin !== origin) return
    const target = getTargetBySource(event.source)
    if (!target) return

    const message = parseProjectFrameChildMessage(event.data)
    if (!message) return

    switch (message.type) {
      case 'pando:title':
        onTitle(target.projectId, message.title)
        break
      case 'pando:busy':
        onBusy(target.projectId, message.busy)
        break
      case 'pando:notification':
        onNotification(target.projectId, message.level, message.message)
        break
      case 'pando:shortcut':
        onShortcut(target.projectId, {
          key: message.key,
          ctrl: message.ctrl,
          alt: message.alt,
          shift: message.shift,
          meta: message.meta,
        })
        break
    }
  }

  hostWindow.addEventListener('message', onMessage)

  const send = (projectId: string, message: ProjectFrameParentMessage) => {
    const target = getTargetByProjectId(projectId)
    if (!target) return
    target.postMessage(message, origin)
  }

  return {
    dispose: () => hostWindow.removeEventListener('message', onMessage),
    send,
    focus: (projectId: string) => send(projectId, { type: 'pando:focus' }),
    sendTheme: (projectId: string, themeId: ThemeId, accent: AccentPreset | null, uiSize: UIScale) =>
      send(projectId, { type: 'pando:theme', themeId, accent, uiSize }),
    sendLanguage: (projectId: string, lang: string) =>
      send(projectId, { type: 'pando:language', lang }),
  }
}

export function createProjectChildBridge({
  hostWindow = window,
  parentWindow = hostWindow.parent,
  onFocus,
  onTheme,
  onLanguage,
}: ProjectFrameChildBridgeOptions) {
  const origin = hostWindow.location.origin

  const onMessage = (event: MessageEvent) => {
    if (event.origin !== origin) return
    if (event.source !== parentWindow) return

    const message = parseProjectFrameParentMessage(event.data)
    if (!message) return

    switch (message.type) {
      case 'pando:focus':
        onFocus()
        break
      case 'pando:theme':
        onTheme(message)
        break
      case 'pando:language':
        onLanguage(message)
        break
    }
  }

  hostWindow.addEventListener('message', onMessage)

  const post = (message: ProjectFrameChildMessage) => {
    if (!parentWindow || typeof (parentWindow as Window).postMessage !== 'function') return
    ;(parentWindow as Window).postMessage(message, origin)
  }

  return {
    dispose: () => hostWindow.removeEventListener('message', onMessage),
    postTitle: (title: string) => post({ type: 'pando:title', title }),
    postBusy: (busy: boolean) => post({ type: 'pando:busy', busy }),
    postNotification: (level: ProjectFrameNotificationLevel, message: string) =>
      post({ type: 'pando:notification', level, message }),
    postShortcut: (shortcut: Omit<ProjectFrameShortcutMessage, 'type'>) =>
      post({ type: 'pando:shortcut', ...shortcut }),
  }
}
