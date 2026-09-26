import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import api from '@pando/client/services/api'
import { Button, Spinner } from '@/components/ui'
import { RefreshCw } from '@/components/ui/icons'
import ModelCombobox from '@/components/shared/ModelCombobox'
import { Notice } from './setupShared'
import { apiErrorMessage } from './setupUtils'

interface ModelsStepProps {
  /** Provider chosen in the previous step, used for the suggestions. */
  providerType: string | null
  currentCoderModel?: string
  onSaved: (mainModel: string, fastModel: string) => void
}

export default function ModelsStep({ providerType, currentCoderModel, onSaved }: ModelsStepProps) {
  const { t } = useTranslation()
  const [mainModel, setMainModel] = useState(currentCoderModel ?? '')
  const [fastModel, setFastModel] = useState('')
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // Remounts the comboboxes so they re-fetch the model list after a refresh.
  const [listKey, setListKey] = useState(0)

  const suggest = async (refresh: boolean) => {
    if (!providerType) return
    setLoading(true)
    setError(null)
    try {
      const r = await api.get<{ mainModel: string; fastModel: string }>(
        `/api/v1/setup/suggested-models?provider=${encodeURIComponent(providerType)}${refresh ? '&refresh=1' : ''}`,
      )
      if (r.mainModel) setMainModel(r.mainModel)
      if (r.fastModel) setFastModel(r.fastModel)
      if (!r.mainModel) setError(t('setup.models.noModels'))
      setListKey((k) => k + 1)
    } catch (e) {
      setError(apiErrorMessage(e, t('setup.models.suggestFailed')))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    // A provider added a moment ago (Copilot login) may not have its models
    // registered yet: ask the server to refresh them first.
    void suggest(true)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [providerType])

  const handleSave = async () => {
    if (!mainModel.trim()) {
      setError(t('setup.models.mainRequired'))
      return
    }
    setSaving(true)
    setError(null)
    try {
      const r = await api.post<{ mainModel: string; fastModel: string }>('/api/v1/setup/models', {
        mainModel: mainModel.trim(),
        fastModel: fastModel.trim(),
      })
      onSaved(r.mainModel, r.fastModel)
    } catch (e) {
      setError(apiErrorMessage(e, t('setup.models.saveFailed')))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <p className="m-0 text-sm text-muted">{t('setup.models.intro')}</p>

      {loading && (
        <div className="flex items-center gap-2 text-sm text-muted">
          <Spinner size={14} />
          {t('setup.models.loading')}
        </div>
      )}

      <div className="flex flex-col gap-1.5">
        <span className="text-sm font-medium text-fg">{t('setup.models.main')}</span>
        <span className="text-xs text-muted">{t('setup.models.mainHint')}</span>
        <ModelCombobox key={`main-${listKey}`} value={mainModel} onChange={setMainModel} placeholder={t('setup.models.pick')} />
      </div>

      <div className="flex flex-col gap-1.5">
        <span className="text-sm font-medium text-fg">{t('setup.models.fast')}</span>
        <span className="text-xs text-muted">{t('setup.models.fastHint')}</span>
        <ModelCombobox key={`fast-${listKey}`} value={fastModel} onChange={setFastModel} placeholder={t('setup.models.sameAsMain')} />
      </div>

      {error && <Notice tone="warning">{error}</Notice>}

      <div className="flex flex-wrap gap-2">
        <Button variant="primary" loading={saving} onClick={() => void handleSave()}>
          {t('setup.models.save')}
        </Button>
        {providerType && (
          <Button variant="ghost" icon={<RefreshCw size={14} />} loading={loading} onClick={() => void suggest(true)}>
            {t('setup.models.refresh')}
          </Button>
        )}
      </div>
    </div>
  )
}
