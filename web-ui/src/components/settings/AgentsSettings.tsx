import { useEffect, useState } from 'react'
import { useAgentsStore } from '@pando/client/stores/settingsStore'
import { useUnsavedChangesGuard } from './unsavedChanges'
import ModelCombobox from '@/components/shared/ModelCombobox'
import type { AgentConfigItem } from '@pando/client/types'
import { SETTINGS_CATEGORY_EVENT } from './settingsEvents'
import api from '@pando/client/services/api'
import { useModelAutoModeStore, type HealthReportDTO } from '@pando/client/stores/modelAutoModeStore'
import { Badge, Button, Card, Input, Select, Switch } from '@/components/ui'
import { ChevronDown, ChevronUp, TriangleAlert } from '@/components/ui/icons'

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



/** Decision-model option of the persona-selector agent (read-only router info + warnings). */
function DecisionModelInfo() {
  const router = useModelAutoModeStore((s) => s.original.router)
  const fetchConfig = useModelAutoModeStore((s) => s.fetchConfig)
  const [health, setHealth] = useState<HealthReportDTO | null>(null)
  const [healthErr, setHealthErr] = useState('')
  const model = router.model.trim()

  useEffect(() => {
    void fetchConfig()
  }, [fetchConfig])

  useEffect(() => {
    if (!model) {
      setHealth(null)
      return
    }
    let cancelled = false
    api
      .get<{ ok: boolean; report?: HealthReportDTO; error?: string }>('/api/v1/model-auto-mode/router/health')
      .then((r) => {
        if (cancelled) return
        setHealth(r.report ?? null)
        setHealthErr(r.report ? '' : (r.error ?? ''))
      })
      .catch((e) => {
        if (!cancelled) setHealthErr(e instanceof Error ? e.message : 'Health check failed')
      })
    return () => {
      cancelled = true
    }
  }, [model, router.provider])

  const openModelAutoMode = () =>
    window.dispatchEvent(new CustomEvent(SETTINGS_CATEGORY_EVENT, { detail: 'model-auto-mode' }))
  const remote = router.provider !== 'ollama'

  return (
    <div className="flex flex-col gap-2" data-testid="persona-decision-info">
      {model ? (
        <div className="flex items-center gap-2 flex-wrap text-sm">
          <span className="text-muted">Router:</span>
          <span className="font-mono">{router.provider}/{model}</span>
          {health && <Badge tone={health.ok ? 'success' : 'danger'}>{health.ok ? 'Healthy' : 'Unhealthy'}</Badge>}
          {!health && healthErr && <Badge tone="danger">{healthErr}</Badge>}
          <Button variant="secondary" onClick={openModelAutoMode}>Open Model auto mode settings</Button>
        </div>
      ) : (
        <div className="settings-banner settings-banner--warning" role="alert">
          <TriangleAlert size={14} />
          <div>
            No router model is configured in Model auto mode, so the fallback model is used.{' '}
            <Button variant="secondary" onClick={openModelAutoMode}>Open Model auto mode settings</Button>
          </div>
        </div>
      )}
      {model && health && !health.ok && health.problems && health.problems.length > 0 && (
        <div className="text-xs text-muted">{health.problems.join('; ')}</div>
      )}
      {model && remote && (
        <div className="settings-banner settings-banner--warning" role="note">
          <TriangleAlert size={14} />
          <div>This provider is remote: your prompts leave your machine to be classified.</div>
        </div>
      )}
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
  const isPersonaSelector = agent.name.toLowerCase() === 'persona-selector'
  const useDecisionModel = isPersonaSelector && !!agent.useDecisionModel
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
          {isPersonaSelector && (
            <div className="flex flex-col gap-3">
              <div className="flex items-center gap-3 pt-1">
                <Switch
                  id="agent-use-decision-model"
                  checked={useDecisionModel}
                  onCheckedChange={(v) => onUpdate({ useDecisionModel: v })}
                />
                <label htmlFor="agent-use-decision-model" className="cursor-pointer">
                  <div className="text-sm font-medium text-fg">Use decision model (from model auto mode)</div>
                  <div className="text-xs text-muted">
                    Pick the persona with the model auto mode router instead of this agent&apos;s model.
                  </div>
                </label>
              </div>
              {useDecisionModel && <DecisionModelInfo />}
            </div>
          )}

          <Field label={useDecisionModel ? 'Fallback model' : 'Model'}>
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
