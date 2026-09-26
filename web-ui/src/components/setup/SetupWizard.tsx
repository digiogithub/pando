import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import clsx from 'clsx'
import api from '@pando/client/services/api'
import { useSetupWizardStore, type SetupStatus } from '@pando/client/stores/setupWizardStore'
import { useConfigInitStore } from '@pando/client/stores/configInitStore'
import { useSettingsStore } from '@pando/client/stores/settingsStore'
import { Badge, Button, Dialog } from '@/components/ui'
import { ArrowLeft, ArrowRight, Check, Folder, Globe } from '@/components/ui/icons'
import ProviderStep from './ProviderStep'
import ModelsStep from './ModelsStep'
import RemembrancesStep from './RemembrancesStep'
import { ChoiceCard, Notice } from './setupShared'
import { apiErrorMessage, shortenPath } from './setupUtils'

type StepId = 'scope' | 'provider' | 'models' | 'remembrances' | 'done'
const STEPS: StepId[] = ['scope', 'provider', 'models', 'remembrances', 'done']
type Scope = 'global' | 'project'

interface Summary {
  scope?: Scope
  configPath?: string
  provider?: string | null
  mainModel?: string
  fastModel?: string
  remembrances?: { documentModel: string; codeModel: string }
}

function StepIndicator({ current }: { current: StepId }) {
  const { t } = useTranslation()
  const index = STEPS.indexOf(current)
  return (
    <ol className="m-0 mb-5 flex list-none items-center gap-1.5 p-0" aria-label={t('setup.stepsLabel')}>
      {STEPS.map((step, i) => (
        <li key={step} className="flex min-w-0 flex-1 flex-col gap-1">
          <div className={clsx('h-1 rounded-pill', i <= index ? 'bg-accent' : 'bg-raised')} />
          <span className={clsx('truncate text-xs', i === index ? 'font-medium text-fg' : 'text-faint')} aria-current={i === index ? 'step' : undefined}>
            {t(`setup.steps.${step}`)}
          </span>
        </li>
      ))}
    </ol>
  )
}

/**
 * First-run setup assistant. Opens by itself when Pando starts in a directory
 * without a project config and no provider/model is configured yet (see
 * config.GetSetupStatus), or on demand from the config banner. Cancelling it
 * at any step leaves the regular screens exactly as they were.
 */
export default function SetupWizard() {
  const { t } = useTranslation()
  const { status, open, fetchStatus, cancel, complete } = useSetupWizardStore()
  const refreshConfigInit = useConfigInitStore((s) => s.fetchStatus)
  const fetchSettings = useSettingsStore((s) => s.fetchSettings)

  const [step, setStep] = useState<StepId>('scope')
  const [scope, setScope] = useState<Scope>('global')
  const [applyingScope, setApplyingScope] = useState(false)
  const [scopeError, setScopeError] = useState<string | null>(null)
  const [providerBusy, setProviderBusy] = useState(false)
  const [summary, setSummary] = useState<Summary>({})

  useEffect(() => {
    void fetchStatus()
  }, [fetchStatus])

  // Each time the assistant opens, start from the first step that applies.
  useEffect(() => {
    if (!open || !status) return
    setSummary({})
    setScopeError(null)
    if (status.hasLocalConfig) {
      // A project config already applies: settings can only go there.
      setSummary({ scope: 'project', configPath: status.localConfigPath })
      setStep('provider')
    } else {
      setScope('global')
      setStep('scope')
    }
    // Only on open: later status refreshes must not reset the flow.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const go = useCallback((next: StepId) => setStep(next), [])

  const applyScope = async () => {
    setApplyingScope(true)
    setScopeError(null)
    try {
      const r = await api.post<{ configPath: string; status: SetupStatus }>('/api/v1/setup/scope', { scope })
      setSummary((s) => ({ ...s, scope, configPath: r.configPath }))
      void refreshConfigInit()
      go('provider')
    } catch (e) {
      setScopeError(apiErrorMessage(e, t('setup.scope.failed')))
    } finally {
      setApplyingScope(false)
    }
  }

  const finish = async () => {
    await complete()
    void refreshConfigInit()
    void fetchSettings()
  }

  if (!open || !status) return null

  const index = STEPS.indexOf(step)
  const firstStep: StepId = status.hasLocalConfig ? 'provider' : 'scope'
  const canGoBack = step !== firstStep && step !== 'done'

  // Optional steps can be skipped; the scope must be chosen first.
  const skipTarget: StepId | null =
    step === 'provider' && status.providerAccounts > 0 ? 'models'
      : step === 'models' ? 'remembrances'
        : step === 'remembrances' ? 'done'
          : null

  const footer = (
    <div className="flex w-full flex-wrap items-center gap-2">
      {step !== 'done' && (
        <Button variant="ghost" onClick={cancel}>
          {t('setup.cancel')}
        </Button>
      )}
      <div className="flex-1" />
      {canGoBack && (
        <Button variant="secondary" icon={<ArrowLeft size={14} />} disabled={providerBusy} onClick={() => go(STEPS[index - 1])}>
          {t('setup.back')}
        </Button>
      )}
      {step === 'scope' && (
        <Button variant="primary" iconRight={<ArrowRight size={14} />} loading={applyingScope} onClick={() => void applyScope()}>
          {t('setup.next')}
        </Button>
      )}
      {skipTarget && (
        <Button variant="secondary" disabled={providerBusy} onClick={() => go(skipTarget)}>
          {t('setup.skip')}
        </Button>
      )}
      {step === 'done' && (
        <Button variant="primary" icon={<Check size={14} />} onClick={() => void finish()}>
          {t('setup.finish')}
        </Button>
      )}
    </div>
  )

  return (
    <Dialog
      open
      onClose={cancel}
      dismissible={false}
      size="lg"
      title={t('setup.title')}
      description={t('setup.subtitle')}
      closeLabel={t('setup.cancel')}
      footer={footer}
    >
      <StepIndicator current={step} />

      {step === 'scope' && (
        <div className="flex flex-col gap-3">
          <p className="m-0 text-sm text-muted">{t('setup.scope.intro')}</p>
          <div className="rounded-sm bg-shell px-3 py-2 font-mono text-xs text-fg">{shortenPath(status.workingDir)}</div>
          <ChoiceCard
            selected={scope === 'global'}
            onClick={() => setScope('global')}
            icon={<Globe size={18} />}
            title={t('setup.scope.global')}
            badge={<Badge tone="accent">{t('setup.scope.default')}</Badge>}
          >
            {t('setup.scope.globalHint')}
            <br />
            <code className="font-mono">{shortenPath(status.globalConfigPath || status.defaultGlobalConfigPath || '~/.config/pando/.pando.toml')}</code>
          </ChoiceCard>
          <ChoiceCard
            selected={scope === 'project'}
            disabled={status.isHomeDir}
            onClick={() => setScope('project')}
            icon={<Folder size={18} />}
            title={t('setup.scope.project')}
          >
            {status.isHomeDir ? t('setup.scope.projectHome') : t('setup.scope.projectHint')}
            <br />
            <code className="font-mono">{shortenPath(`${status.workingDir}/.pando.toml`)}</code>
          </ChoiceCard>
          {scopeError && <Notice tone="danger">{scopeError}</Notice>}
        </div>
      )}

      {step === 'provider' && (
        <ProviderStep
          setBusy={setProviderBusy}
          onConfigured={(provider) => {
            setSummary((s) => ({ ...s, provider }))
            void fetchStatus()
            go('models')
          }}
          onKeepExisting={(provider) => {
            setSummary((s) => ({ ...s, provider }))
            go('models')
          }}
        />
      )}

      {step === 'models' && (
        <ModelsStep
          providerType={summary.provider ?? null}
          currentCoderModel={status.coderModelValid ? status.coderModel : undefined}
          onSaved={(mainModel, fastModel) => {
            setSummary((s) => ({ ...s, mainModel, fastModel }))
            void fetchStatus()
            go('remembrances')
          }}
        />
      )}

      {step === 'remembrances' && (
        <RemembrancesStep
          onEnabled={(documentModel, codeModel) => {
            setSummary((s) => ({ ...s, remembrances: { documentModel, codeModel } }))
            go('done')
          }}
        />
      )}

      {step === 'done' && (
        <div className="flex flex-col gap-3">
          <Notice tone="success">{t('setup.done.intro')}</Notice>
          <dl className="m-0 grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
            <dt className="text-muted">{t('setup.done.scope')}</dt>
            <dd className="m-0 text-fg">
              {summary.scope ? t(`setup.scope.${summary.scope}`) : '—'}
              {summary.configPath && <span className="ml-2 font-mono text-xs text-muted">{shortenPath(summary.configPath)}</span>}
            </dd>
            <dt className="text-muted">{t('setup.done.provider')}</dt>
            <dd className="m-0 text-fg">{summary.provider ?? t('setup.done.unchanged')}</dd>
            <dt className="text-muted">{t('setup.done.mainModel')}</dt>
            <dd className="m-0 font-mono text-xs text-fg">{summary.mainModel ?? t('setup.done.unchanged')}</dd>
            <dt className="text-muted">{t('setup.done.fastModel')}</dt>
            <dd className="m-0 font-mono text-xs text-fg">{summary.fastModel ?? t('setup.done.unchanged')}</dd>
            <dt className="text-muted">{t('setup.done.remembrances')}</dt>
            <dd className="m-0 text-fg">
              {summary.remembrances ? (
                <span className="font-mono text-xs">
                  {summary.remembrances.documentModel} · {summary.remembrances.codeModel}
                </span>
              ) : (
                t('setup.done.skipped')
              )}
            </dd>
          </dl>
          <p className="m-0 text-xs text-faint">{t('setup.done.settingsHint')}</p>
        </div>
      )}
    </Dialog>
  )
}
