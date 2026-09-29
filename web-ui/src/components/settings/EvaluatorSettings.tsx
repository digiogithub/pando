import { useEffect, useRef, useState } from 'react'
import { useExtensionsStore } from '@pando/client/stores/extensionsStore'
import type { EvaluatorWeights } from '@pando/client/types'
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

  const weights: EvaluatorWeights = evaluator.weights ?? {
    success: evaluator.alphaWeight,
    tokens: evaluator.betaWeight,
    toolErrors: 0.1,
    cancels: 0.05,
    repetition: 0.05,
    turns: 0.05,
    endState: 0.1,
  }
  const setWeight = (key: keyof EvaluatorWeights, v: number) =>
    updateEvaluator({ weights: { ...weights, [key]: v } })
  const judge = evaluator.judge ?? {}
  const setJudge = (patch: Partial<NonNullable<typeof evaluator.judge>>) =>
    updateEvaluator({ judge: { ...judge, ...patch } })
  const trimmer = evaluator.contextTrimmer ?? { enabled: false }

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
        <SliderInput label="Success (corrections)" value={weights.success} onChange={(v) => setWeight('success', v)} />
        <SliderInput label="Token efficiency" value={weights.tokens} onChange={(v) => setWeight('tokens', v)} />
        <SliderInput label="Tool errors" value={weights.toolErrors} onChange={(v) => setWeight('toolErrors', v)} />
        <SliderInput label="Cancelled runs" value={weights.cancels} onChange={(v) => setWeight('cancels', v)} />
        <SliderInput label="Repeated tool calls" value={weights.repetition} onChange={(v) => setWeight('repetition', v)} />
        <SliderInput label="Turns to completion" value={weights.turns} onChange={(v) => setWeight('turns', v)} />
        <SliderInput label="Ended right after an error" value={weights.endState} onChange={(v) => setWeight('endState', v)} />
        <div className="p-4">
          <div className="settings-banner">
            Weights are relative: the reward is the weighted mean of the signals measured for each session.
            Explicit /feedback overrides the total (bad below 0.3, good above 0.8).
          </div>
        </div>
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

      <SettingsSection title="When sessions are evaluated">
        <SettingsRow
          label="Idle timeout"
          description="A session with no new message for this long is scored by the idle sweeper (Go duration, e.g. 30m, 2h). Sessions are also scored when you switch away from them and at shutdown."
          htmlFor="evaluator-idle-timeout"
        >
          <Input
            id="evaluator-idle-timeout"
            value={evaluator.idleTimeout ?? '30m'}
            onChange={(e) => updateEvaluator({ idleTimeout: e.target.value })}
            placeholder="30m"
          />
        </SettingsRow>
        <SettingsRow
          label="Backfill limit"
          description="Historical unscored sessions evaluated at startup (primary instance only). 0 uses the default 50; a negative value disables the backfill."
          htmlFor="evaluator-backfill-limit"
        >
          <Input
            id="evaluator-backfill-limit"
            type="number"
            step={1}
            value={evaluator.backfillLimit ?? 50}
            onChange={(e) => updateEvaluator({ backfillLimit: parseInt(e.target.value, 10) || 0 })}
          />
        </SettingsRow>
        <SettingsRow label="Judge during backfill" description="Also run the LLM judge on backfilled sessions (costs judge calls; off by default)" htmlFor="evaluator-backfill-judge">
          <Switch id="evaluator-backfill-judge" checked={evaluator.backfillJudge ?? false} onCheckedChange={(v) => updateEvaluator({ backfillJudge: v })} />
        </SettingsRow>
        <SettingsRow label="Include subagent sessions" description="Also evaluate delegated (child) sessions. Off by default: only root sessions are scored." htmlFor="evaluator-include-subagents">
          <Switch id="evaluator-include-subagents" checked={evaluator.includeSubagents ?? false} onCheckedChange={(v) => updateEvaluator({ includeSubagents: v })} />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title="LLM judge gating and budget">
        <SliderInput label="High reward band (judge at or above)" value={judge.highReward ?? 0.8} onChange={(v) => setJudge({ highReward: v })} />
        <SliderInput label="Low reward band (judge at or below)" value={judge.lowReward ?? 0.3} onChange={(v) => setJudge({ lowReward: v })} />
        <SettingsRow label="Minimum user turns" description="The judge only runs for sessions with at least this many user turns." htmlFor="evaluator-judge-min-turns">
          <Input id="evaluator-judge-min-turns" type="number" min={1} step={1} value={judge.minTurns ?? 4} onChange={(e) => setJudge({ minTurns: parseInt(e.target.value, 10) || 0 })} />
        </SettingsRow>
        <SettingsRow label="Transcript cap (tokens)" description="The judge prompt keeps the head and tail of the transcript within this budget." htmlFor="evaluator-judge-max-transcript">
          <Input id="evaluator-judge-max-transcript" type="number" min={500} step={500} value={judge.maxTranscriptTokens ?? 6000} onChange={(e) => setJudge({ maxTranscriptTokens: parseInt(e.target.value, 10) || 0 })} />
        </SettingsRow>
        <SettingsRow label="Daily judge calls" description="Maximum judge calls per local day. 0 means unlimited." htmlFor="evaluator-judge-daily-calls">
          <Input id="evaluator-judge-daily-calls" type="number" min={0} step={1} value={judge.dailyCalls ?? 20} onChange={(e) => setJudge({ dailyCalls: parseInt(e.target.value, 10) || 0 })} />
        </SettingsRow>
        <SettingsRow label="Daily judge tokens" description="Maximum estimated judge tokens (prompt plus completion) per local day. 0 means unlimited." htmlFor="evaluator-judge-daily-tokens">
          <Input id="evaluator-judge-daily-tokens" type="number" min={0} step={1000} value={judge.dailyTokens ?? 200000} onChange={(e) => setJudge({ dailyTokens: parseInt(e.target.value, 10) || 0 })} />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title="Prompt variants and context trimming">
        <SettingsRow
          label="Prompt variant selection"
          description="A/B test the variant files under .pando/prompts/variants/<section>/<name>.md.tpl. Inert until such files exist."
          htmlFor="evaluator-templates-enabled"
        >
          <Switch id="evaluator-templates-enabled" checked={evaluator.templates?.enabled ?? true} onCheckedChange={(v) => updateEvaluator({ templates: { enabled: v } })} />
        </SettingsRow>
        <SettingsRow
          label="Context trimmer"
          description="Experimental: an extra LLM call on every new session that filters the tools shown to the model. Off by default."
          htmlFor="evaluator-trimmer-enabled"
        >
          <Switch id="evaluator-trimmer-enabled" checked={trimmer.enabled} onCheckedChange={(v) => updateEvaluator({ contextTrimmer: { ...trimmer, enabled: v } })} />
        </SettingsRow>
        <SliderInput
          label="Trimmer minimum confidence"
          value={trimmer.minConfidence ?? 0.7}
          onChange={(v) => updateEvaluator({ contextTrimmer: { ...trimmer, minConfidence: v } })}
        />
      </SettingsSection>

      <SettingsSection title="Correction patterns">
        <div className="p-4">
          <TagListEditor
            items={evaluator.correctionsPatterns ?? []}
            onChange={(v) => updateEvaluator({ correctionsPatterns: v })}
            placeholder="regex pattern…"
          />
          <div className="settings-banner mt-3">
            Use single backslashes (for example <code>(?i)\bwrong\b</code>). A doubled backslash matches a literal
            backslash and never fires. <code>pando evaluator doctor</code> flags these.
          </div>
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
