import { useEffect, useState } from 'react'
import { useExtensionsStore } from '@/stores/extensionsStore'
import { Toggle, TextInput } from '@/components/shared/FormInput'
import ModelCombobox from '@/components/shared/ModelCombobox'



const dividerStyle: React.CSSProperties = {
  borderTop: '1px solid var(--border)',
  margin: '1.5rem 0',
}

// ---- Inline TagListEditor for patterns ----

function TagListEditor({
  label,
  values,
  onChange,
  placeholder = 'Add pattern…',
}: {
  label: string
  values: string[]
  onChange: (v: string[]) => void
  placeholder?: string
}) {
  const [input, setInput] = useState('')

  const add = () => {
    const trimmed = input.trim()
    if (trimmed && !values.includes(trimmed)) {
      onChange([...values, trimmed])
    }
    setInput('')
  }

  const remove = (idx: number) => {
    onChange(values.filter((_, i) => i !== idx))
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
      <label
        style={{
          fontSize: 12,
          fontWeight: 600,
          color: 'var(--fg-muted)',
          textTransform: 'uppercase',
          letterSpacing: '0.04em',
        }}
      >
        {label}
      </label>
      {values.length > 0 && (
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.4rem' }}>
          {values.map((v, i) => (
            <span
              key={i}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: '0.3rem',
                background: 'var(--selected)',
                border: '1px solid var(--border)',
                borderRadius: 'var(--radius-sm)',
                padding: '0.2rem 0.5rem',
                fontSize: 13,
                color: 'var(--fg)',
                fontFamily: 'monospace',
              }}
            >
              {v}
              <button
                onClick={() => remove(i)}
                style={{
                  background: 'none',
                  border: 'none',
                  cursor: 'pointer',
                  color: 'var(--fg-muted)',
                  padding: 0,
                  lineHeight: 1,
                  fontSize: 14,
                  fontFamily: 'inherit',
                }}
                aria-label={`Remove ${v}`}
              >
                ×
              </button>
            </span>
          ))}
        </div>
      )}
      <div style={{ display: 'flex', gap: '0.5rem' }}>
        <input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              add()
            }
          }}
          placeholder={placeholder}
          style={{
            flex: 1,
            background: 'var(--input-bg)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            color: 'var(--fg)',
            fontSize: 14,
            padding: '0.4rem 0.75rem',
            outline: 'none',
            fontFamily: 'monospace',
            boxSizing: 'border-box',
          }}
          onFocus={(e) => (e.target.style.borderColor = 'var(--border-focus)')}
          onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
        />
        <button
          onClick={add}
          style={{
            padding: '0.4rem 1rem',
            background: 'var(--primary)',
            color: 'var(--primary-fg)',
            border: 'none',
            borderRadius: 'var(--radius-sm)',
            fontSize: 14,
            cursor: 'pointer',
            fontFamily: 'inherit',
          }}
        >
          Add
        </button>
      </div>
    </div>
  )
}

// ---- Slider with numeric display ----
function SliderInput({
  label,
  value,
  onChange,
  min = 0,
  max = 1,
  step = 0.05,
}: {
  label: string
  value: number
  onChange: (v: number) => void
  min?: number
  max?: number
  step?: number
}) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
      <label
        style={{
          fontSize: 12,
          fontWeight: 600,
          color: 'var(--fg-muted)',
          textTransform: 'uppercase',
          letterSpacing: '0.04em',
        }}
      >
        {label}
      </label>
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
        <input
          type="range"
          min={min}
          max={max}
          step={step}
          value={value}
          onChange={(e) => onChange(parseFloat(e.target.value))}
          style={{ flex: 1, accentColor: 'var(--primary)' }}
        />
        <span
          style={{
            minWidth: 36,
            textAlign: 'right',
            fontSize: 14,
            color: 'var(--fg)',
            fontVariantNumeric: 'tabular-nums',
          }}
        >
          {value.toFixed(2)}
        </span>
      </div>
    </div>
  )
}

// ---- Main component ----
export default function EvaluatorSettings() {
  const {
    evaluator,
    evaluatorDirty,
    evaluatorLoading,
    evaluatorSaving,
    evaluatorError,
    fetchEvaluator,
    updateEvaluator,
    saveEvaluator,
    resetEvaluator,
  } = useExtensionsStore()

  useEffect(() => {
    fetchEvaluator()
  }, [fetchEvaluator])

  const weightSum = evaluator.alphaWeight + evaluator.betaWeight
  const weightWarning = Math.abs(weightSum - 1.0) > 0.01

  if (evaluatorLoading) {
    return (
      <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 14 }}>
        Loading evaluator settings…
      </div>
    )
  }

  return (
    <div style={{ maxWidth: 640 }}>
      <h2 style={{ fontSize: 18, fontWeight: 700, color: 'var(--fg)', marginBottom: '1.25rem' }}>
        Evaluator
      </h2>

      <div style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
        <Toggle
          label="Enabled"
          description="Activate the self-improvement evaluation loop (LLM-as-Judge)"
          checked={evaluator.enabled}
          onChange={(v) => updateEvaluator({ enabled: v })}
        />

        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
          <label
            style={{
              fontSize: 12,
              fontWeight: 600,
              color: 'var(--fg-muted)',
              textTransform: 'uppercase',
              letterSpacing: '0.04em',
            }}
          >
            Judge Model
          </label>
          <ModelCombobox
            value={evaluator.model}
            onChange={(v) => updateEvaluator({ model: v })}
            onSelect={(m) => updateEvaluator({ provider: m.provider })}
          />
          <p style={{ fontSize: 12, color: 'var(--fg-dim)', margin: 0 }}>
            Select the model that will act as the judge for performance metrics.
          </p>
        </div>



        <div style={dividerStyle} />

        {/* Reward weights */}
        <p
          style={{
            fontSize: 13,
            fontWeight: 600,
            color: 'var(--fg-muted)',
            textTransform: 'uppercase',
            letterSpacing: '0.04em',
            margin: 0,
          }}
        >
          Reward Weights
        </p>

        <SliderInput
          label="Alpha — Accuracy weight"
          value={evaluator.alphaWeight}
          onChange={(v) => updateEvaluator({ alphaWeight: v })}
        />

        <SliderInput
          label="Beta — Efficiency weight"
          value={evaluator.betaWeight}
          onChange={(v) => updateEvaluator({ betaWeight: v })}
        />

        {weightWarning && (
          <div
            style={{
              padding: '0.5rem 0.75rem',
              background: 'rgba(255,165,0,0.1)',
              border: '1px solid rgba(255,165,0,0.4)',
              borderRadius: 'var(--radius-sm)',
              fontSize: 13,
              color: 'var(--fg)',
            }}
          >
            ⚠ Alpha + Beta = {weightSum.toFixed(2)} (ideally should sum to 1.0)
          </div>
        )}

        <div style={dividerStyle} />

        {/* UCB / misc settings */}
        <p
          style={{
            fontSize: 13,
            fontWeight: 600,
            color: 'var(--fg-muted)',
            textTransform: 'uppercase',
            letterSpacing: '0.04em',
            margin: 0,
          }}
        >
          UCB Settings
        </p>

        {/* UCB Exploration Factor */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
          <label
            style={{
              fontSize: 12,
              fontWeight: 600,
              color: 'var(--fg-muted)',
              textTransform: 'uppercase',
              letterSpacing: '0.04em',
            }}
          >
            UCB Exploration Factor
          </label>
          <input
            type="number"
            min={0}
            step={0.1}
            value={evaluator.explorationC}
            onChange={(e) => updateEvaluator({ explorationC: parseFloat(e.target.value) || 0 })}
            style={{
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
            }}
            onFocus={(e) => (e.target.style.borderColor = 'var(--border-focus)')}
            onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
          />
        </div>

        <TextInput
          label="Judge Prompt Template"
          placeholder="/path/to/judge-prompt.md"
          value={evaluator.judgePromptTemplate}
          onChange={(e) => updateEvaluator({ judgePromptTemplate: e.target.value })}
        />

        <Toggle
          label="Async Evaluation"
          description="Run evaluation in the background after session end (recommended)"
          checked={evaluator.async}
          onChange={(v) => updateEvaluator({ async: v })}
        />

        <div style={dividerStyle} />

        <TagListEditor
          label="Correction Patterns"
          values={evaluator.correctionsPatterns ?? []}
          onChange={(v) => updateEvaluator({ correctionsPatterns: v })}
          placeholder="regex pattern…"
        />
      </div>

      <div style={dividerStyle} />

      {evaluatorError && (
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
          {evaluatorError}
        </div>
      )}

      <div style={{ display: 'flex', gap: '0.75rem' }}>
        <button
          onClick={saveEvaluator}
          disabled={!evaluatorDirty || evaluatorSaving}
          style={{
            padding: '0.5rem 1.5rem',
            background: !evaluatorDirty || evaluatorSaving ? 'var(--border)' : 'var(--primary)',
            color: !evaluatorDirty || evaluatorSaving ? 'var(--fg-muted)' : 'var(--primary-fg)',
            border: 'none',
            borderRadius: 'var(--radius-sm)',
            fontSize: 14,
            fontWeight: 600,
            cursor: !evaluatorDirty || evaluatorSaving ? 'not-allowed' : 'pointer',
            fontFamily: 'inherit',
          }}
        >
          {evaluatorSaving ? 'Saving…' : 'Save'}
        </button>
        <button
          onClick={resetEvaluator}
          disabled={!evaluatorDirty}
          style={{
            padding: '0.5rem 1.5rem',
            background: 'transparent',
            color: !evaluatorDirty ? 'var(--fg-dim)' : 'var(--fg-muted)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            fontSize: 14,
            fontWeight: 600,
            cursor: !evaluatorDirty ? 'not-allowed' : 'pointer',
            fontFamily: 'inherit',
          }}
        >
          Reset
        </button>
      </div>
    </div>
  )
}
