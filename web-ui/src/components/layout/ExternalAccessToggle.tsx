import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faGlobe } from '@fortawesome/free-solid-svg-icons'
import { useToastStore } from '@pando/client/stores/toastStore'
import api from '@pando/client/services/api'

interface ExternalAccessStatus {
  enabled: boolean
  bindHost: string
  port: number
  canToggle: boolean
  basicAuthReady: boolean
  urls: string[]
}

// ExternalAccessToggle flips the running server between a loopback bind and
// 0.0.0.0, so the same instance can be reached from other devices while `pando
// app` / `pando desktop` keeps running. The bind is runtime-only: it is never
// written to the config file, so a restart is local again.
export default function ExternalAccessToggle() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const toast = useToastStore.getState().addToast
  const [status, setStatus] = useState<ExternalAccessStatus | null>(null)
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    try {
      setStatus(await api.get<ExternalAccessStatus>('/api/v1/config/api-server/external-access'))
    } catch {
      // The endpoint is missing on older backends; the toggle simply stays hidden.
      setStatus(null)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const toggle = useCallback(async () => {
    if (!status || busy) return

    if (!status.enabled && !status.basicAuthReady) {
      toast(t('externalAccess.needsCredentials'), 'warning', 6000)
      navigate('/settings')
      return
    }

    setBusy(true)
    try {
      const next = await api.put<ExternalAccessStatus>(
        '/api/v1/config/api-server/external-access',
        { enabled: !status.enabled },
      )
      setStatus(next)
      if (next.enabled) {
        const where = next.urls.length > 0 ? next.urls.join(', ') : `:${next.port}`
        toast(t('externalAccess.enabledToast', { urls: where }), 'success', 8000)
      } else {
        toast(t('externalAccess.disabledToast'), 'info')
      }
    } catch (e) {
      toast(e instanceof Error ? e.message : t('externalAccess.toggleFailed'), 'error')
    } finally {
      setBusy(false)
    }
  }, [busy, navigate, status, t, toast])

  if (!status || (!status.canToggle && !status.enabled)) return null

  const fixed = !status.canToggle
  const title = status.enabled
    ? t('externalAccess.onTooltip', {
        urls: status.urls.length > 0 ? status.urls.join('\n') : `${status.bindHost}:${status.port}`,
      })
    : t('externalAccess.offTooltip')

  return (
    <button
      onClick={() => { void toggle() }}
      disabled={busy || fixed}
      title={fixed ? t('externalAccess.fixedTooltip') : title}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '0.375rem',
        background: status.enabled ? 'var(--success, #16a34a)' : 'none',
        border: 'none',
        cursor: busy || fixed ? 'default' : 'pointer',
        color: status.enabled ? '#fff' : 'var(--fg-muted)',
        fontSize: 11,
        fontWeight: status.enabled ? 600 : 400,
        opacity: busy ? 0.6 : 1,
        padding: '0 0.5rem',
        height: 18,
        borderRadius: 'var(--radius-sm)',
        transition: 'color 0.15s, background 0.15s',
      }}
    >
      <FontAwesomeIcon icon={faGlobe} style={{ fontSize: 10 }} />
      <span>{status.enabled ? t('externalAccess.on') : t('externalAccess.off')}</span>
    </button>
  )
}
