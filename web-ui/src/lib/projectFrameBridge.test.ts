import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  createProjectChildBridge,
  createProjectFrameParentBridge,
} from './projectFrameBridge'

describe('projectFrameBridge', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('validates origin and source before routing child messages to the parent', () => {
    const frameSource = { postMessage: vi.fn() } as unknown as MessageEventSource
    const parentTarget = {
      projectId: 'proj-1',
      source: frameSource,
      postMessage: vi.fn(),
    }
    const onTitle = vi.fn()
    const onBusy = vi.fn()
    const onNotification = vi.fn()
    const onShortcut = vi.fn()

    const bridge = createProjectFrameParentBridge({
      getTargetBySource: (source) => (source === frameSource ? parentTarget : null),
      getTargetByProjectId: () => null,
      onTitle,
      onBusy,
      onNotification,
      onShortcut,
    })

    window.dispatchEvent(new MessageEvent('message', {
      origin: 'https://example.invalid',
      source: frameSource,
      data: { type: 'pando:title', title: 'Ignored' },
    }))
    window.dispatchEvent(new MessageEvent('message', {
      origin: window.location.origin,
      source: window,
      data: { type: 'pando:title', title: 'Ignored' },
    }))

    expect(onTitle).not.toHaveBeenCalled()

    window.dispatchEvent(new MessageEvent('message', {
      origin: window.location.origin,
      source: frameSource,
      data: { type: 'pando:title', title: 'Child session' },
    }))
    window.dispatchEvent(new MessageEvent('message', {
      origin: window.location.origin,
      source: frameSource,
      data: { type: 'pando:busy', busy: true },
    }))
    window.dispatchEvent(new MessageEvent('message', {
      origin: window.location.origin,
      source: frameSource,
      data: { type: 'pando:notification', level: 'warning', message: 'Heads up' },
    }))
    window.dispatchEvent(new MessageEvent('message', {
      origin: window.location.origin,
      source: frameSource,
      data: { type: 'pando:shortcut', key: 'p', ctrl: true, alt: false, shift: false, meta: false },
    }))

    expect(onTitle).toHaveBeenCalledWith('proj-1', 'Child session')
    expect(onBusy).toHaveBeenCalledWith('proj-1', true)
    expect(onNotification).toHaveBeenCalledWith('proj-1', 'warning', 'Heads up')
    expect(onShortcut).toHaveBeenCalledWith('proj-1', {
      key: 'p',
      ctrl: true,
      alt: false,
      shift: false,
      meta: false,
    })

    bridge.dispose()
  })

  it('routes messages in both directions for the selected frame and child window', () => {
    const framePostMessage = vi.fn()
    const frameSource = { postMessage: framePostMessage } as unknown as MessageEventSource
    const onFocus = vi.fn()
    const onTheme = vi.fn()
    const onLanguage = vi.fn()
    const parentPostMessage = vi.fn()
    const parentWindow = { postMessage: parentPostMessage } as unknown as Window

    const parentBridge = createProjectFrameParentBridge({
      getTargetBySource: () => null,
      getTargetByProjectId: (projectId) =>
        projectId === 'proj-1'
          ? {
              projectId,
              source: frameSource,
              postMessage: framePostMessage,
            }
          : null,
      onTitle: vi.fn(),
      onBusy: vi.fn(),
      onNotification: vi.fn(),
      onShortcut: vi.fn(),
    })

    parentBridge.focus('proj-1')
    parentBridge.sendTheme('proj-1', 'pando-dark', 'blue', 'large')
    parentBridge.sendLanguage('proj-1', 'ja')

    expect(framePostMessage).toHaveBeenNthCalledWith(1, { type: 'pando:focus' }, window.location.origin)
    expect(framePostMessage).toHaveBeenNthCalledWith(2, {
      type: 'pando:theme',
      themeId: 'pando-dark',
      accent: 'blue',
      uiSize: 'large',
    }, window.location.origin)
    expect(framePostMessage).toHaveBeenNthCalledWith(3, {
      type: 'pando:language',
      lang: 'ja',
    }, window.location.origin)

    const childBridge = createProjectChildBridge({
      parentWindow,
      onFocus,
      onTheme,
      onLanguage,
    })

    childBridge.postTitle('Nested terminal')
    childBridge.postBusy(true)
    childBridge.postNotification('info', 'Linked')
    childBridge.postShortcut({ key: '1', ctrl: true, alt: true, shift: false, meta: false })

    expect(parentPostMessage).toHaveBeenNthCalledWith(1, {
      type: 'pando:title',
      title: 'Nested terminal',
    }, window.location.origin)
    expect(parentPostMessage).toHaveBeenNthCalledWith(2, {
      type: 'pando:busy',
      busy: true,
    }, window.location.origin)
    expect(parentPostMessage).toHaveBeenNthCalledWith(3, {
      type: 'pando:notification',
      level: 'info',
      message: 'Linked',
    }, window.location.origin)
    expect(parentPostMessage).toHaveBeenNthCalledWith(4, {
      type: 'pando:shortcut',
      key: '1',
      ctrl: true,
      alt: true,
      shift: false,
      meta: false,
    }, window.location.origin)

    window.dispatchEvent(new MessageEvent('message', {
      origin: 'https://example.invalid',
      source: parentWindow,
      data: { type: 'pando:focus' },
    }))
    window.dispatchEvent(new MessageEvent('message', {
      origin: window.location.origin,
      source: window,
      data: { type: 'pando:focus' },
    }))

    expect(onFocus).not.toHaveBeenCalled()

    window.dispatchEvent(new MessageEvent('message', {
      origin: window.location.origin,
      source: parentWindow,
      data: { type: 'pando:focus' },
    }))
    window.dispatchEvent(new MessageEvent('message', {
      origin: window.location.origin,
      source: parentWindow,
      data: { type: 'pando:theme', themeId: 'paper-light', accent: null, uiSize: 'small' },
    }))
    window.dispatchEvent(new MessageEvent('message', {
      origin: window.location.origin,
      source: parentWindow,
      data: { type: 'pando:language', lang: 'de' },
    }))

    expect(onFocus).toHaveBeenCalledTimes(1)
    expect(onTheme).toHaveBeenCalledWith({
      type: 'pando:theme',
      themeId: 'paper-light',
      accent: null,
      uiSize: 'small',
    })
    expect(onLanguage).toHaveBeenCalledWith({
      type: 'pando:language',
      lang: 'de',
    })

    childBridge.dispose()
    parentBridge.dispose()
  })
})
