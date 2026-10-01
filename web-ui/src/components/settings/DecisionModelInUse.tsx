import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { isHostedDecisionProvider, useDecisionModelStore } from '@pando/client/stores/decisionModelStore'
import { SETTINGS_CATEGORY_EVENT } from './settingsEvents'
import { Badge, Button } from '@/components/ui'
import { TriangleAlert } from '@/components/ui/icons'

/** The server caches the router health for 60 s, so polling faster is pointless. */
const HEALTH_POLL_MS = 60_000

const DECISION_MODEL_CATEGORY = 'decision-model'

function openDecisionModelSettings() {
  window.dispatchEvent(new CustomEvent(SETTINGS_CATEGORY_EVENT, { detail: DECISION_MODEL_CATEGORY }))
}

interface Props {
  /** True when the feature that shows this row is switched on; an empty model then warns. */
  consumerEnabled: boolean
  /** What happens while no decision model is configured (already translated). */
  missingNote?: string
  /** Replaces the default privacy hint shown for a hosted provider (already translated). */
  hostedNote?: string
  testId?: string
}

/**
 * Read-only "decision model in use" row shared by every feature that depends on
 * the shared decision model (Auto mode, persona auto-select, context filter).
 * It shows the saved provider/model with live health and links to the page that
 * edits it.
 */
export default function DecisionModelInUse({ consumerEnabled, missingNote, hostedNote, testId = 'decision-model-in-use' }: Props) {
  const { t } = useTranslation()
  const router = useDecisionModelStore((s) => s.original.router)
  const effectiveBaseURL = useDecisionModelStore((s) => s.info.effectiveBaseURL)
  const health = useDecisionModelStore((s) => s.health)
  const healthError = useDecisionModelStore((s) => s.healthError)
  const fetchConfig = useDecisionModelStore((s) => s.fetchConfig)
  const fetchHealth = useDecisionModelStore((s) => s.fetchHealth)
  const loaded = useDecisionModelStore((s) => s.loaded)
  const model = router.model.trim()

  useEffect(() => {
    void fetchConfig()
  }, [fetchConfig])

  useEffect(() => {
    if (!loaded) return
    void fetchHealth()
    if (!model) return
    const id = window.setInterval(() => void fetchHealth(), HEALTH_POLL_MS)
    return () => window.clearInterval(id)
  }, [loaded, model, router.provider, fetchHealth])

  const configure = (
    <Button variant="secondary" onClick={openDecisionModelSettings}>
      {t('settings.decisionModel.inUse.configure', 'Configure')} →
    </Button>
  )
  const hosted = isHostedDecisionProvider(router.provider, effectiveBaseURL)

  return (
    <div className="flex flex-col gap-2" data-testid={testId}>
      {model ? (
        <div className="flex items-center gap-2 flex-wrap text-sm">
          <span className="text-muted">{t('settings.decisionModel.inUse.label', 'Decision model')}:</span>
          <span className="font-mono">{router.provider}/{model}</span>
          {health && (
            <Badge tone={health.ok ? 'success' : 'danger'}>
              {health.ok ? t('settings.decisionModel.health.healthy', 'Healthy') : t('settings.decisionModel.health.unhealthy', 'Unhealthy')}
            </Badge>
          )}
          {!health && healthError && <Badge tone="danger">{healthError}</Badge>}
          {configure}
        </div>
      ) : consumerEnabled ? (
        <div className="settings-banner settings-banner--warning" role="alert">
          <TriangleAlert size={14} />
          <div>
            {t('settings.decisionModel.inUse.notConfigured', 'No decision model is configured.')}{' '}
            {missingNote}{' '}
            {configure}
          </div>
        </div>
      ) : (
        <div className="flex items-center gap-2 flex-wrap text-sm">
          <span className="text-muted">{t('settings.decisionModel.inUse.label', 'Decision model')}:</span>
          <span className="text-muted">{t('settings.decisionModel.inUse.none', 'not configured')}</span>
          {configure}
        </div>
      )}
      {model && health && !health.ok && health.problems && health.problems.length > 0 && (
        <div className="text-xs text-muted">{health.problems.join('; ')}</div>
      )}
      {model && hosted && (
        <div className="settings-banner settings-banner--warning" role="note">
          <TriangleAlert size={14} />
          <div>
            {hostedNote ??
              t(
                'settings.decisionModel.inUse.hosted',
                'This provider is remote: your prompts leave your machine to be processed by the decision model.',
              )}
          </div>
        </div>
      )}
    </div>
  )
}
