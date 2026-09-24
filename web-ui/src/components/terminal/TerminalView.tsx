import clsx from 'clsx'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useTerminalStore } from '@pando/client/stores/terminalStore'
import { sendPtyInput } from '@pando/client/services/terminalPty'
import TerminalOutput from './TerminalOutput'
import TerminalInput from './TerminalInput'
import TerminalPtyPane from './TerminalPtyPane'
import { Button, Spinner } from '@/components/ui'
import { Plus, SquareTerminal, Trash2, X } from '@/components/ui/icons'
import '@/styles/terminal.css'

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
    <div className="terminal-shell" onMouseDown={() => setFocusKey((value) => value + 1)}>
      <div className="terminal-header">
        <div className="terminal-header-left">
          <div className="terminal-header-title">
            <SquareTerminal size={15} />
            <span className="terminal-header-title-text">Terminal</span>
            {activeTab.running && (
              <div className="terminal-running">
                <Spinner size={12} />
                <span>running…</span>
              </div>
            )}
          </div>

          <div className="terminal-tabs">
            {tabs.map((tab) => {
              const active = tab.id === activeTab.id
              return (
                <button
                  key={tab.id}
                  type="button"
                  onClick={() => setActiveTab(tab.id)}
                  onMouseDown={(event) => {
                    if (event.button === 1) {
                      event.preventDefault()
                      event.stopPropagation()
                      closeTab(tab.id)
                    }
                  }}
                  className={clsx('terminal-tab', active && 'terminal-tab--active')}
                >
                  <SquareTerminal size={11} className={active ? 'terminal-tab-icon--active' : undefined} />
                  <span>{tab.title}</span>
                  {tab.running && <Spinner size={10} />}
                  <span
                    role="button"
                    aria-label={`Close ${tab.title}`}
                    onClick={(event) => {
                      event.stopPropagation()
                      closeTab(tab.id)
                    }}
                    className="terminal-tab-close"
                  >
                    <X size={11} />
                  </span>
                </button>
              )
            })}
            <button type="button" onClick={createTab} title="New terminal tab" className="terminal-new-btn">
              <Plus size={11} />
              <span>New</span>
            </button>
          </div>
        </div>

        <Button size="sm" variant="ghost" icon={<Trash2 size={12} />} disabled={clearDisabled} onClick={handleClear} title="Clear terminal">
          Clear
        </Button>
      </div>

      <div className="terminal-body">
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
              <div className="terminal-fallback-banner">
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
