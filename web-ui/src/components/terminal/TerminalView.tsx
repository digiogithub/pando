import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faPlus, faTerminal, faTrash, faXmark } from '@fortawesome/free-solid-svg-icons'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useTerminalStore } from '@pando/client/stores/terminalStore'
import { sendPtyInput } from '@pando/client/services/terminalPty'
import TerminalOutput from './TerminalOutput'
import TerminalInput from './TerminalInput'
import TerminalPtyPane from './TerminalPtyPane'
import LoadingSpinner from '@/components/shared/LoadingSpinner'

export default function TerminalView() {
  const { tabs, activeTabId, setActiveTab, createTab, closeTab, clearEntries } = useTerminalStore()
  const [focusKey, setFocusKey] = useState(0)
  const { t } = useTranslation()

  const activeTab = useMemo(() => {
    return tabs.find((tab) => tab.id === activeTabId) ?? tabs[0]
  }, [activeTabId, tabs])

  if (!activeTab) {
    return null
  }

  // In PTY mode the shell owns the screen, so clearing means asking it to clear
  // (Ctrl+L) rather than dropping entries the store no longer holds.
  const clearDisabled = activeTab.mode === 'pty' ? !activeTab.ptySessionId : activeTab.entries.length === 0
  const handleClear = () => {
    if (activeTab.mode === 'pty') {
      sendPtyInput(activeTab.id, '\x0c')
      return
    }
    clearEntries(activeTab.id)
  }

  return (
    <div
      onMouseDown={() => setFocusKey((value) => value + 1)}
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        minHeight: 0,
        overflow: 'hidden',
        background: '#0d1117',
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '0.625rem 1rem',
          background: '#161b22',
          borderBottom: '1px solid #30363d',
          flexShrink: 0,
          gap: '1rem',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', minWidth: 0, flex: 1 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexShrink: 0 }}>
            <FontAwesomeIcon icon={faTerminal} style={{ color: '#3fb950', fontSize: 13 }} />
            <span style={{ fontSize: 13, fontWeight: 600, color: '#e2e8f0' }}>Terminal</span>
            {activeTab.running && (
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.375rem' }}>
                <LoadingSpinner size={12} />
                <span style={{ fontSize: 11, color: '#6e7681' }}>running…</span>
              </div>
            )}
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: '0.35rem', overflowX: 'auto', minWidth: 0 }}>
            {tabs.map((tab) => {
              const active = tab.id === activeTab.id
              return (
                <button
                  key={tab.id}
                  onClick={() => setActiveTab(tab.id)}
                  onMouseDown={(event) => {
                    if (event.button === 1) {
                      event.preventDefault()
                      event.stopPropagation()
                      closeTab(tab.id)
                    }
                  }}
                  style={{
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: '0.4rem',
                    padding: '0.3rem 0.6rem',
                    borderRadius: 6,
                    border: `1px solid ${active ? '#58a6ff' : '#30363d'}`,
                    background: active ? '#1f2937' : '#0d1117',
                    color: active ? '#e6edf3' : '#8b949e',
                    cursor: 'pointer',
                    whiteSpace: 'nowrap',
                  }}
                >
                  <FontAwesomeIcon icon={faTerminal} style={{ fontSize: 10, color: active ? '#3fb950' : undefined }} />
                  <span style={{ fontSize: 12 }}>{tab.title}</span>
                  {tab.running && <LoadingSpinner size={10} />}
                  <span
                    onClick={(event) => {
                      event.stopPropagation()
                      closeTab(tab.id)
                    }}
                    style={{ display: 'inline-flex', alignItems: 'center', color: '#6e7681' }}
                  >
                    <FontAwesomeIcon icon={faXmark} style={{ fontSize: 10 }} />
                  </span>
                </button>
              )
            })}
            <button
              onClick={createTab}
              title="New terminal tab"
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: '0.35rem',
                padding: '0.3rem 0.6rem',
                borderRadius: 6,
                border: '1px solid #30363d',
                background: '#0d1117',
                color: '#8b949e',
                cursor: 'pointer',
                whiteSpace: 'nowrap',
              }}
            >
              <FontAwesomeIcon icon={faPlus} style={{ fontSize: 10 }} />
              <span style={{ fontSize: 12 }}>New</span>
            </button>
          </div>
        </div>

        <button
          onClick={handleClear}
          title="Clear terminal"
          disabled={clearDisabled}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '0.3rem',
            background: 'none',
            border: '1px solid #30363d',
            borderRadius: 'var(--radius-sm)',
            padding: '0.25rem 0.6rem',
            cursor: clearDisabled ? 'not-allowed' : 'pointer',
            color: clearDisabled ? '#6e7681' : '#8b949e',
            fontSize: 12,
            flexShrink: 0,
          }}
        >
          <FontAwesomeIcon icon={faTrash} style={{ fontSize: 10 }} />
          Clear
        </button>
      </div>

      <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        {/* Every PTY tab stays mounted, hidden with CSS when inactive, so its
            shell keeps running and its screen survives tab switches. */}
        {tabs
          .filter((tab) => tab.mode === 'pty')
          .map((tab) => (
            <TerminalPtyPane key={tab.id} tab={tab} active={tab.id === activeTab.id} />
          ))}

        {activeTab.mode === 'exec' && (
          <>
            {activeTab.fallbackReason && (
              <div
                style={{
                  padding: '0.4rem 0.75rem',
                  background: '#2d2211',
                  borderBottom: '1px solid #9e6a03',
                  color: '#d29922',
                  fontSize: 11,
                  flexShrink: 0,
                }}
              >
                {t('terminal.ptyUnavailable', { reason: activeTab.fallbackReason })}
              </div>
            )}
            <TerminalOutput entries={activeTab.entries} shell={activeTab.shell} cwd={activeTab.cwd} />
            <TerminalInput tab={activeTab} focusKey={focusKey} />
          </>
        )}
      </div>
    </div>
  )
}
