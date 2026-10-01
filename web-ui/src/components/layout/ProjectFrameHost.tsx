import { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { useSettingsStore } from '@pando/client/stores/settingsStore'
import { useProjectTabsStore } from '@pando/client/stores/projectTabsStore'
import { useToastStore } from '@pando/client/stores/toastStore'
import { useTheme } from '@/hooks/useTheme'
import { readWorkspaceProjectId } from '@/hooks/useProjectTabRouteSync'
import { createProjectFrameParentBridge } from '@/lib/projectFrameBridge'
import { useUIScale } from '@/components/settings/uiScale'
import {
  handleShellKeyboardShortcut,
  type ProjectTabBarController,
  type ShellShortcutActions,
} from './ProjectTabBarControls'

interface CachedFrame {
  projectId: string
  name: string
  webUrl: string
}

function focusFrame(frame: HTMLIFrameElement) {
  if (typeof navigator !== 'undefined' && /jsdom/i.test(navigator.userAgent)) {
    return
  }
  frame.focus()
  try {
    frame.contentWindow?.focus()
  } catch {
    // Browsers may reject this if the frame is not ready yet.
  }
}

export default function ProjectFrameHost({
  controller,
  shortcutActions,
}: {
  controller: ProjectTabBarController
  shortcutActions: ShellShortcutActions
}) {
  const location = useLocation()
  const tabs = useProjectTabsStore((state) => state.tabs)
  const focusRequestId = useProjectTabsStore((state) => state.focusRequestId)
  const language = useSettingsStore((state) => state.config.language)
  const uiSize = useUIScale()
  const { themeId, accent } = useTheme()

  const frameRefs = useRef(new Map<string, HTMLIFrameElement>())
  const [frames, setFrames] = useState<CachedFrame[]>([])

  useEffect(() => {
    setFrames((current) => {
      const next = new Map(current.map((frame) => [frame.projectId, frame]))
      const openIds = new Set(tabs.map((tab) => tab.projectId))

      for (const projectId of next.keys()) {
        if (!openIds.has(projectId)) {
          next.delete(projectId)
          frameRefs.current.delete(projectId)
        }
      }

      for (const tab of tabs) {
        if (tab.state !== 'running' || !tab.webUrl) continue
        next.set(tab.projectId, {
          projectId: tab.projectId,
          name: tab.name,
          webUrl: tab.webUrl,
        })
      }

      return tabs
        .map((tab) => next.get(tab.projectId))
        .filter((frame): frame is CachedFrame => Boolean(frame))
    })
  }, [tabs])

  useEffect(() => {
    const bridge = createProjectFrameParentBridge({
      getTargetBySource: (source) => {
        for (const [projectId, frame] of frameRefs.current.entries()) {
          if (frame.contentWindow === source) {
            return {
              projectId,
              source,
              postMessage: (message, targetOrigin) => frame.contentWindow?.postMessage(message, targetOrigin),
            }
          }
        }
        return null
      },
      getTargetByProjectId: (projectId) => {
        const frame = frameRefs.current.get(projectId)
        if (!frame || !frame.contentWindow) return null
        return {
          projectId,
          source: frame.contentWindow,
          postMessage: (message, targetOrigin) => frame.contentWindow?.postMessage(message, targetOrigin),
        }
      },
      onTitle: (projectId, title) =>
        useProjectTabsStore.getState().setRuntimeState(projectId, { childTitle: title }),
      onBusy: (projectId, busy) =>
        useProjectTabsStore.getState().setRuntimeState(projectId, { busy }),
      onNotification: (_projectId, level, message) =>
        useToastStore.getState().addToast(message, level),
      onShortcut: (_projectId, shortcut) => {
        handleShellKeyboardShortcut(
          {
            ctrlKey: shortcut.ctrl,
            altKey: shortcut.alt,
            metaKey: shortcut.meta,
            shiftKey: shortcut.shift,
            key: shortcut.key,
          },
          controller,
          shortcutActions,
        )
      },
    })

    return () => bridge.dispose()
  }, [controller, shortcutActions])

  const workspaceProjectId = readWorkspaceProjectId(location.pathname)
  const activeRunningTab = useMemo(
    () =>
      workspaceProjectId
        ? tabs.find((tab) => tab.projectId === workspaceProjectId && tab.state === 'running')
        : null,
    [tabs, workspaceProjectId],
  )

  useEffect(() => {
    if (!workspaceProjectId || !activeRunningTab) return

    const frame = frameRefs.current.get(workspaceProjectId)
    if (!frame?.contentWindow) return

    focusFrame(frame)
    frame.contentWindow.postMessage({ type: 'pando:focus' }, window.location.origin)
  }, [activeRunningTab, focusRequestId, workspaceProjectId])

  useEffect(() => {
    for (const [projectId, frame] of frameRefs.current.entries()) {
      if (!frame.contentWindow) continue
      frame.contentWindow.postMessage(
        { type: 'pando:theme', themeId, accent, uiSize },
        window.location.origin,
      )
      frame.contentWindow.postMessage(
        { type: 'pando:language', lang: language },
        window.location.origin,
      )
      if (projectId === workspaceProjectId && activeRunningTab) {
        frame.contentWindow.postMessage({ type: 'pando:focus' }, window.location.origin)
      }
    }
  }, [accent, activeRunningTab, language, themeId, uiSize, workspaceProjectId])

  if (frames.length === 0) return null

  return (
    <div className="shell-projectframes" aria-hidden={workspaceProjectId ? undefined : true}>
      {frames.map((frame) => {
        const visible = frame.projectId === workspaceProjectId && activeRunningTab?.projectId === frame.projectId
        return (
          <iframe
            key={frame.projectId}
            ref={(element) => {
              if (element) {
                frameRefs.current.set(frame.projectId, element)
              } else {
                frameRefs.current.delete(frame.projectId)
              }
            }}
            src={frame.webUrl}
            title={frame.name}
            className="shell-projectframe"
            data-visible={visible || undefined}
            allow="clipboard-read; clipboard-write"
            hidden={!visible}
            inert={!visible}
            onLoad={() => {
              const current = frameRefs.current.get(frame.projectId)
              if (!current?.contentWindow) return
              current.contentWindow.postMessage(
                { type: 'pando:theme', themeId, accent, uiSize },
                window.location.origin,
              )
              current.contentWindow.postMessage(
                { type: 'pando:language', lang: language },
                window.location.origin,
              )
              if (frame.projectId === workspaceProjectId && activeRunningTab?.projectId === frame.projectId) {
                current.contentWindow.postMessage({ type: 'pando:focus' }, window.location.origin)
              }
            }}
          />
        )
      })}
    </div>
  )
}
