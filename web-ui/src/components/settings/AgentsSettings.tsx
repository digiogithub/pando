import { useEffect, useState } from 'react'
import { useAgentsStore } from '@pando/client/stores/settingsStore'
import { useUnsavedChangesGuard } from './unsavedChanges'
import ModelCombobox from '@/components/shared/ModelCombobox'
import type { AgentConfigItem } from '@pando/client/types'
import { Badge, Button, Card, Input, Select, Switch } from '@/components/ui'
import { ChevronDown, ChevronUp } from '@/components/ui/icons'

const AGENT_NAMES = ['coder', 'summarizer', 'task', 'title', 'cli-assist', 'persona-selector', 'context-enricher']

const AGENT_LABELS: Record<string, string> = {
  coder: 'Coder',
  summarizer: 'Summarizer',
  task: 'Task',
  title: 'Title',
  'cli-assist': 'CLI Assist',
  'persona-selector': 'Persona Selector',
  'context-enricher': 'Content Enricher',
}

// Automatic token budgets per agent role (mirrors config.AutoBudgetByRole)
const AUTO_TOKEN_BUDGET: Record<string, number> = {
  title: 80,
  'persona-selector': 64,
  'cli-assist': 256,
  'context-enricher': 256,
  task: 2048,
  summarizer: 4096,
  coder: 8192,
}

const AGENT_DESCRIPTIONS: Record<string, string> = {
  coder: 'Main coding and problem-solving agent',
  summarizer: 'Summarizes sessions and content',
  task: 'Manages and executes tasks',
  title: 'Generates session titles',
  'cli-assist': 'Assists with CLI and terminal tasks',
  'persona-selector': 'Selects and switches personas automatically',
  'context-enricher': 'Plans and enriches prompts with relevant remembered context before retrieval',
}

const REASONING_EFFORT_OPTIONS = [
  { value: '', label: 'Default' },
  { value: 'none', label: 'None' },
  { value: 'low', label: 'Low' },
  { value: 'medium', label: 'Medium' },
  { value: 'high', label: 'High' },
]

const THINKING_MODE_OPTIONS = [
  { value: '', label: 'Default' },
  { value: 'disabled', label: 'Disabled' },
  { value: 'low', label: 'Low (20% budget)' },
  { value: 'medium', label: 'Medium (50% budget)' },
  { value: 'high', label: 'High (80% budget)' },
]

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="settings-field">
      <label className="settings-field-label">{label}</label>
      {children}
    </div>
  )
}

function AgentCard({
  agent,
  onUpdate,
}: {
  agent: AgentConfigItem
  onUpdate: (patch: Partial<AgentConfigItem>) => void
}) {
  const [expanded, setExpanded] = useState(false)
  const [modelProvider, setModelProvider] = useState('')
  const label = AGENT_LABELS[agent.name.toLowerCase()] ?? agent.name
  const description = AGENT_DESCRIPTIONS[agent.name.toLowerCase()] ?? ''
  // Token budget / auto-compaction only matter for the agent driving the long
  // agent loop (coder). The backend reports this via contextControls; the name
  // check is the fallback for older backends.
  const showContextControls = agent.contextControls ?? agent.name.toLowerCase() === 'coder'

  return (
    <Card padding="none">
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
        className="w-full flex items-center gap-3 px-4 py-3.5 bg-transparent border-0 text-left cursor-pointer font-sans"
      >
        <div className="flex-1 min-w-0">
          <div className="text-md font-semibold text-fg">{label}</div>
          {description && <div className="text-xs text-muted mt-0.5">{description}</div>}
        </div>
        {agent.model && (
          <Badge tone="accent" className="font-mono">
            {modelProvider && <span className="text-faint font-normal">{modelProvider}/</span>}
            {agent.model}
          </Badge>
        )}
        {expanded ? <ChevronUp size={14} className="text-muted" /> : <ChevronDown size={14} className="text-muted" />}
      </button>

      {expanded && (
        <div className="flex flex-col gap-4 px-4 pb-4 pt-3 border-t border-border">
          <Field label="Model">
            <ModelCombobox
              value={agent.model}
              onChange={(v) => onUpdate({ model: v })}
              onSelect={(m) => setModelProvider(m.provider)}
            />
          </Field>

          <div className={`grid gap-4 ${showContextControls ? 'grid-cols-1 sm:grid-cols-3' : 'grid-cols-1 sm:grid-cols-2'}`}>
            {showContextControls && (
              <Field label="Max tokens">
                <Input
                  type="number"
                  min={0}
                  value={agent.maxTokens}
                  onChange={(e) => onUpdate({ maxTokens: parseInt(e.target.value, 10) || 0 })}
                />
                {agent.maxTokens === 0 ? (
                  <div className="text-xs text-muted">
                    Auto — effective: {agent.resolvedMaxTokens ?? AUTO_TOKEN_BUDGET[agent.name.toLowerCase()] ?? 4096} tokens
                  </div>
                ) : (
                  <div className="text-xs text-muted">0 = Auto</div>
                )}
              </Field>
            )}

            <Field label="Reasoning effort">
              <Select
                options={REASONING_EFFORT_OPTIONS}
                value={agent.reasoningEffort}
                onChange={(e) => onUpdate({ reasoningEffort: e.target.value })}
              />
            </Field>

            <Field label="Thinking mode">
              <Select
                options={THINKING_MODE_OPTIONS}
                value={agent.thinkingMode ?? ''}
                onChange={(e) => onUpdate({ thinkingMode: e.target.value })}
              />
            </Field>
          </div>

          {showContextControls && (
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 items-start">
              <div className="flex items-center gap-3 pt-1">
                <Switch
                  id="agent-auto-compact"
                  checked={agent.autoCompact}
                  onCheckedChange={(v) => onUpdate({ autoCompact: v })}
                />
                <label htmlFor="agent-auto-compact" className="cursor-pointer">
                  <div className="text-sm font-medium text-fg">Auto-compact</div>
                  <div className="text-xs text-muted">Compress context automatically</div>
                </label>
              </div>

              <Field label="Compact threshold">
                <Input
                  type="number"
                  min={0}
                  max={1}
                  step={0.05}
                  value={agent.autoCompactThreshold}
                  onChange={(e) => onUpdate({ autoCompactThreshold: parseFloat(e.target.value) || 0 })}
                />
              </Field>
            </div>
          )}
        </div>
      )}
    </Card>
  )
}

export default function AgentsSettings() {
  const { agents, dirty, loading, saving, error, fetchAgents, updateAgent, saveAgents, resetAgents } =
    useAgentsStore()
  useUnsavedChangesGuard({
    id: 'agents',
    dirty,
    save: async () => {
      await saveAgents()
      return !useAgentsStore.getState().error
    },
    discard: resetAgents,
  })

  useEffect(() => {
    fetchAgents()
  }, [fetchAgents])

  if (loading) {
    return <div className="settings-loading">Loading agents…</div>
  }

  // Merge known agent names with what the backend returned
  const agentMap = new Map(agents.map((a) => [a.name.toLowerCase(), a]))

  const displayAgents: AgentConfigItem[] = AGENT_NAMES.map(
    (name) =>
      agentMap.get(name) ?? {
        name,
        model: '',
        maxTokens: 0,
        reasoningEffort: '',
        thinkingMode: '',
        autoCompact: false,
        autoCompactThreshold: 0,
        contextControls: name === 'coder',
      }
  )

  // Only the canonical built-in agents (AGENT_NAMES) are shown. The backend
  // already restricts its response to these, so any stray/legacy agent key is
  // never rendered as a phantom duplicate.

  return (
    <div>
      <header className="settings-page-header">
        <h2 className="settings-page-title">Agents</h2>
        <p className="settings-page-description">
          Configure model and behavior for each built-in agent. Changes apply to new sessions.
        </p>
      </header>

      <div className="flex flex-col gap-3">
        {displayAgents.map((agent) => (
          <AgentCard
            key={agent.name}
            agent={agent}
            onUpdate={(patch) => updateAgent(agent.name, patch)}
          />
        ))}
      </div>

      {error && <div className="settings-banner settings-banner--danger mt-4" role="alert">{error}</div>}

      <div className="settings-actions">
        <Button variant="primary" onClick={saveAgents} disabled={!dirty || saving} loading={saving}>
          {saving ? 'Saving…' : 'Save'}
        </Button>
        <Button variant="secondary" onClick={resetAgents} disabled={!dirty}>
          Reset
        </Button>
      </div>
    </div>
  )
}
