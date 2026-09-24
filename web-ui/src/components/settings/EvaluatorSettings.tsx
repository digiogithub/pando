import { useEffect, useRef, useState } from 'react'
import { useExtensionsStore } from '@pando/client/stores/extensionsStore'
import { useUnsavedChangesGuard } from './unsavedChanges'
import ModelCombobox from '@/components/shared/ModelCombobox'
import TagListEditor from '@/components/shared/TagListEditor'
import { Button, IconButton, Input, SettingsRow, SettingsSection, Switch, Textarea } from '@/components/ui'
import { X } from '@/components/ui/icons'

// Default judge prompt template shown in the UI when no custom path is configured.
const DEFAULT_JUDGE_PROMPT = `You are an expert AI assistant evaluator. Analyze this conversation transcript between an AI coding assistant and a user.

Template used: {{.TemplateName}} (version {{.TemplateVersion}})
User corrections detected: {{.Corrections}}
Total tokens used: {{.Tokens}}

Analyze the transcript and respond ONLY with a valid JSON object (no markdown, no explanation outside JSON):
{
  "reasoning": "brief explanation of what worked or did not work",
  "key_points": ["point1", "point2", "point3"],
  "new_skill": "optional: a 1-2 line instruction rule that would improve future sessions (empty string if none)",
  "task_type": "one of: code, refactor, debug, explain, general",
  "confidence": 0.0
}

Focus on quality dimensions: scope compliance, step-by-step adherence, constraint handling,
anti-patterns (unrequested scripts/features), iterative corrections, and context utilisation.

TRANSCRIPT:
{{.Transcript}}`

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
    <SettingsRow label={label}>
      <div className="flex items-center gap-3 w-full">
        <input
          type="range"
          min={min}
          max={max}
          step={step}
          value={value}
          onChange={(e) => onChange(parseFloat(e.target.value))}
          className="flex-1 accent-[var(--accent)]"
        />
        <span className="min-w-9 text-right text-sm text-fg tabular-nums">{value.toFixed(2)}</span>
      </div>
    </SettingsRow>
  )
}

// ---- Judge Prompt Template field ----
function JudgePromptTemplateField({
  value,
  onChange,
}: {
  value: string
  onChange: (v: string) => void
}) {
  const [showDefault, setShowDefault] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)

  return (
    <SettingsRow
      label="Judge prompt template"
      description={
        <>
          Path to a custom Go template file (<code>.md</code> or <code>.txt</code>). Leave empty to use the built-in
          default template.
        </>
      }
      stacked
    >
      <div className="flex gap-2">
        <Input
          className="flex-1 font-mono"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="Leave empty to use built-in default template"
        />
        <input
          ref={fileInputRef}
          type="file"
          accept=".md,.txt,.tmpl,.tpl"
          className="hidden"
          onChange={(e) => {
            const file = e.target.files?.[0]
            if (file) {
              // In a browser we only get the filename; show it so the user knows
              // which file was selected and can type/confirm the full path.
              onChange(file.name)
            }
            // Reset so the same file can be re-selected if needed
            e.target.value = ''
          }}
        />
        <Button variant="secondary" onClick={() => fileInputRef.current?.click()} title="Browse for a template file">
          Browse…
        </Button>
        {value && (
          <IconButton aria-label="Revert to built-in default" tooltip icon={<X size={14} />} onClick={() => onChange('')} />
        )}
      </div>

      <Button variant="ghost" size="sm" className="self-start" onClick={() => setShowDefault((v) => !v)}>
        {showDefault ? 'Hide' : 'Show'} built-in default template
      </Button>

      {showDefault && <Textarea readOnly value={DEFAULT_JUDGE_PROMPT} rows={12} className="font-mono text-xs" />}
    </SettingsRow>
  )
}

// ---- Main component ----
export default function SelfImprovementSettings() {
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
  useUnsavedChangesGuard({
    id: 'self-improvement',
    dirty: evaluatorDirty,
    save: async () => {
      await saveEvaluator()
      return !useExtensionsStore.getState().evaluatorError
    },
    discard: resetEvaluator,
  })

  useEffect(() => {
    fetchEvaluator()
  }, [fetchEvaluator])

  const weightSum = evaluator.alphaWeight + evaluator.betaWeight
  const weightWarning = Math.abs(weightSum - 1.0) > 0.01

  if (evaluatorLoading) {
    return <div className="settings-loading">Loading evaluator settings…</div>
  }

  return (
    <div>
      <header className="settings-page-header">
        <h2 className="settings-page-title">Self-Improvement</h2>
      </header>

      <SettingsSection>
        <SettingsRow label="Enabled" description="Activate the self-improvement evaluation loop (LLM-as-Judge)" htmlFor="evaluator-enabled">
          <Switch id="evaluator-enabled" checked={evaluator.enabled} onCheckedChange={(v) => updateEvaluator({ enabled: v })} />
        </SettingsRow>
        <SettingsRow label="Judge model" description="Select the model that will act as the judge for performance metrics." stacked>
          <ModelCombobox
            value={evaluator.model}
            onChange={(v) => updateEvaluator({ model: v })}
            onSelect={(m) => updateEvaluator({ provider: m.provider })}
          />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title="Reward weights">
        <SliderInput label="Alpha — accuracy weight" value={evaluator.alphaWeight} onChange={(v) => updateEvaluator({ alphaWeight: v })} />
        <SliderInput label="Beta — efficiency weight" value={evaluator.betaWeight} onChange={(v) => updateEvaluator({ betaWeight: v })} />
        {weightWarning && (
          <div className="p-4">
            <div className="settings-banner settings-banner--warning">
              Alpha + Beta = {weightSum.toFixed(2)} (ideally should sum to 1.0)
            </div>
          </div>
        )}
      </SettingsSection>

      <SettingsSection title="UCB settings">
        <SettingsRow label="UCB exploration factor" htmlFor="evaluator-exploration-c">
          <Input
            id="evaluator-exploration-c"
            type="number"
            min={0}
            step={0.1}
            value={evaluator.explorationC}
            onChange={(e) => updateEvaluator({ explorationC: parseFloat(e.target.value) || 0 })}
          />
        </SettingsRow>
        <JudgePromptTemplateField
          value={evaluator.judgePromptTemplate}
          onChange={(v) => updateEvaluator({ judgePromptTemplate: v })}
        />
        <SettingsRow label="Async evaluation" description="Run evaluation in the background after session end (recommended)" htmlFor="evaluator-async">
          <Switch id="evaluator-async" checked={evaluator.async} onCheckedChange={(v) => updateEvaluator({ async: v })} />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title="Correction patterns">
        <div className="p-4">
          <TagListEditor
            items={evaluator.correctionsPatterns ?? []}
            onChange={(v) => updateEvaluator({ correctionsPatterns: v })}
            placeholder="regex pattern…"
          />
        </div>
      </SettingsSection>

      {evaluatorError && <div className="settings-banner settings-banner--danger" role="alert">{evaluatorError}</div>}

      <div className="settings-actions">
        <Button variant="primary" onClick={saveEvaluator} disabled={!evaluatorDirty || evaluatorSaving} loading={evaluatorSaving}>
          {evaluatorSaving ? 'Saving…' : 'Save'}
        </Button>
        <Button variant="secondary" onClick={resetEvaluator} disabled={!evaluatorDirty}>
          Reset
        </Button>
      </div>
    </div>
  )
}
