import { useEffect, useState } from 'react'
import { useAgentsStore } from '@/stores/settingsStore'
import type { AgentConfigItem } from '@/types'

const AGENT_NAMES = ['coder', 'summarizer', 'task', 'title', 'cliassist', 'persona']

const AGENT_LABELS: Record<string, string> = {
  coder: 'Coder',
  summarizer: 'Summarizer',
  task: 'Task',
  title: 'Title',
  cliassist: 'CLI Assist',
  persona: 'Persona Selector',
}

const AGENT_DESCRIPTIONS: Record<string, string> = {
  coder: 'Main coding and problem-solving agent',
  summarizer: 'Summarizes sessions and content',
  task: 'Manages and executes tasks',
  title: 'Generates session titles',
  cliassist: 'Assists with CLI and terminal tasks',
  persona: 'Selects and switches personas automatically',
}

const REASONING_EFFORT_OPTIONS = [
  { value: '', label: 'Default' },
  { value: 'none', label: 'None' },
  { value: 'low', label: 'Low' },
  { value: 'medium', label: 'Medium' },
  { value: 'high', label: 'High' },
]

const sectionTitle: React.CSSProperties = {
  fontSize: 18,
  fontWeight: 700,
  color: 'var(--fg)',
  marginBottom: '1.25rem',
}

const dividerStyle: React.CSSProperties = {
  borderTop: '1px solid var(--border)',
  margin: '1.5rem 0',
}

const labelStyle: React.CSSProperties = {
  fontSize: 12,
  fontWeight: 600,
  color: 'var(--fg-muted)',
  textTransform: 'uppercase',
  letterSpacing: '0.04em',
}

const inputStyle: React.CSSProperties = {
  background: 'var(--input-bg)',
  border: '1px solid var(--border)',
  borderRadius: 'var(--radius-sm)',
  color: 'var(--fg)',
  fontSize: 14,
  padding: '0.5rem 0.75rem',
  outline: 'none',
  width: '100%',
  fontFamily: 'inherit',
  boxSizing: 'border-box',
  transition: 'border-color 0.15s',
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
      <label style={labelStyle}>{label}</label>
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
  const label = AGENT_LABELS[agent.name.toLowerCase()] ?? agent.name
  const description = AGENT_DESCRIPTIONS[agent.name.toLowerCase()] ?? ''

  return (
    <div
      style={{
        border: '1px solid var(--border)',
        borderRadius: 'var(--radius)',
        overflow: 'hidden',
        background: 'var(--card-bg, var(--input-bg))',
      }}
    >
      {/* Header */}
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        style={{
          width: '100%',
          display: 'flex',
          alignItems: 'center',
          gap: '0.75rem',
          padding: '0.875rem 1rem',
          background: 'none',
          border: 'none',
          cursor: 'pointer',
          textAlign: 'left',
          fontFamily: 'inherit',
        }}
      >
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: 15, fontWeight: 600, color: 'var(--fg)' }}>{label}</div>
          {description && (
            <div style={{ fontSize: 12, color: 'var(--fg-muted)', marginTop: 2 }}>{description}</div>
          )}
        </div>
        {agent.model && (
          <span
            style={{
              fontSize: 11,
              fontWeight: 600,
              padding: '0.2rem 0.5rem',
              borderRadius: 'var(--radius-sm)',
              background: 'rgba(99,102,241,0.12)',
              color: 'var(--primary)',
              fontFamily: 'monospace',
            }}
          >
            {agent.model}
          </span>
        )}
        <span
          style={{
            color: 'var(--fg-muted)',
            fontSize: 12,
            marginLeft: '0.5rem',
            transform: expanded ? 'rotate(180deg)' : 'none',
            transition: 'transform 0.2s',
            display: 'inline-block',
          }}
        >
          ▼
        </span>
      </button>

      {/* Collapsible form */}
      {expanded && (
        <div
          style={{
            padding: '0 1rem 1rem',
            display: 'flex',
            flexDirection: 'column',
            gap: '1rem',
            borderTop: '1px solid var(--border)',
          }}
        >
          <div style={{ height: '0.75rem' }} />

          {/* Model */}
          <Field label="Model">
            <input
              type="text"
              value={agent.model}
              placeholder="e.g. claude-sonnet-4-6"
              onChange={(e) => onUpdate({ model: e.target.value })}
              style={inputStyle}
              onFocus={(e) => { e.target.style.borderColor = 'var(--border-focus)' }}
              onBlur={(e) => { e.target.style.borderColor = 'var(--border)' }}
            />
          </Field>

          {/* MaxTokens + Reasoning Effort in a row */}
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
            <Field label="Max Tokens">
              <input
                type="number"
                min={0}
                value={agent.maxTokens}
                onChange={(e) => onUpdate({ maxTokens: parseInt(e.target.value, 10) || 0 })}
                style={inputStyle}
                onFocus={(e) => { e.target.style.borderColor = 'var(--border-focus)' }}
                onBlur={(e) => { e.target.style.borderColor = 'var(--border)' }}
              />
            </Field>

            <Field label="Reasoning Effort">
              <select
                value={agent.reasoningEffort}
                onChange={(e) => onUpdate({ reasoningEffort: e.target.value })}
                style={{ ...inputStyle, cursor: 'pointer' }}
                onFocus={(e) => { e.currentTarget.style.borderColor = 'var(--border-focus)' }}
                onBlur={(e) => { e.currentTarget.style.borderColor = 'var(--border)' }}
              >
                {REASONING_EFFORT_OPTIONS.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </Field>
          </div>

          {/* Auto-compact */}
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem', alignItems: 'start' }}>
            <div
              role="switch"
              aria-checked={agent.autoCompact}
              tabIndex={0}
              style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', cursor: 'pointer', paddingTop: '0.25rem' }}
              onClick={() => onUpdate({ autoCompact: !agent.autoCompact })}
              onKeyDown={(e) => {
                if (e.key === ' ' || e.key === 'Enter') {
                  e.preventDefault()
                  onUpdate({ autoCompact: !agent.autoCompact })
                }
              }}
            >
              {/* Track */}
              <div
                style={{
                  width: 36,
                  height: 20,
                  borderRadius: 10,
                  background: agent.autoCompact ? 'var(--primary)' : 'var(--border)',
                  position: 'relative',
                  transition: 'background 0.2s',
                  flexShrink: 0,
                }}
              >
                <div
                  style={{
                    width: 16,
                    height: 16,
                    borderRadius: '50%',
                    background: 'white',
                    position: 'absolute',
                    top: 2,
                    left: agent.autoCompact ? 18 : 2,
                    transition: 'left 0.2s',
                    boxShadow: '0 1px 3px rgba(0,0,0,0.2)',
                  }}
                />
              </div>
              <div>
                <div style={{ fontSize: 14, color: 'var(--fg)', fontWeight: 500 }}>Auto-compact</div>
                <div style={{ fontSize: 12, color: 'var(--fg-muted)' }}>Compress context automatically</div>
              </div>
            </div>

            <Field label="Compact Threshold">
              <input
                type="number"
                min={0}
                max={1}
                step={0.05}
                value={agent.autoCompactThreshold}
                onChange={(e) => onUpdate({ autoCompactThreshold: parseFloat(e.target.value) || 0 })}
                style={inputStyle}
                onFocus={(e) => { e.target.style.borderColor = 'var(--border-focus)' }}
                onBlur={(e) => { e.target.style.borderColor = 'var(--border)' }}
              />
            </Field>
          </div>
        </div>
      )}
    </div>
  )
}

export default function AgentsSettings() {
  const { agents, dirty, loading, saving, error, fetchAgents, updateAgent, saveAgents, resetAgents } =
    useAgentsStore()

  useEffect(() => {
    fetchAgents()
  }, [fetchAgents])

  if (loading) {
    return (
      <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 14 }}>
        Loading agents…
      </div>
    )
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
        autoCompact: false,
        autoCompactThreshold: 0,
      }
  )

  // Also include any additional agents from the backend that aren't in our known list
  agents.forEach((a) => {
    if (!AGENT_NAMES.includes(a.name.toLowerCase())) {
      displayAgents.push(a)
    }
  })

  return (
    <div style={{ maxWidth: 640 }}>
      <h2 style={sectionTitle}>Agents</h2>
      <p style={{ fontSize: 14, color: 'var(--fg-muted)', marginBottom: '1.5rem' }}>
        Configure model and behavior for each built-in agent. Changes apply to new sessions.
      </p>

      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
        {displayAgents.map((agent) => (
          <AgentCard
            key={agent.name}
            agent={agent}
            onUpdate={(patch) => updateAgent(agent.name, patch)}
          />
        ))}
      </div>

      <div style={dividerStyle} />

      {error && (
        <div
          style={{
            marginBottom: '1rem',
            padding: '0.625rem 0.875rem',
            background: 'var(--error)',
            color: 'var(--primary-fg)',
            borderRadius: 'var(--radius-sm)',
            fontSize: 13,
          }}
        >
          {error}
        </div>
      )}

      <div style={{ display: 'flex', gap: '0.75rem' }}>
        <button
          onClick={saveAgents}
          disabled={!dirty || saving}
          style={{
            padding: '0.5rem 1.5rem',
            background: !dirty || saving ? 'var(--border)' : 'var(--primary)',
            color: !dirty || saving ? 'var(--fg-muted)' : 'var(--primary-fg)',
            border: 'none',
            borderRadius: 'var(--radius-sm)',
            fontSize: 14,
            fontWeight: 600,
            cursor: !dirty || saving ? 'not-allowed' : 'pointer',
            transition: 'background 0.15s',
            fontFamily: 'inherit',
          }}
        >
          {saving ? 'Saving…' : 'Save'}
        </button>

        <button
          onClick={resetAgents}
          disabled={!dirty}
          style={{
            padding: '0.5rem 1.5rem',
            background: 'transparent',
            color: !dirty ? 'var(--fg-dim)' : 'var(--fg-muted)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            fontSize: 14,
            fontWeight: 600,
            cursor: !dirty ? 'not-allowed' : 'pointer',
            transition: 'color 0.15s',
            fontFamily: 'inherit',
          }}
        >
          Reset
        </button>
      </div>
    </div>
  )
}
