import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import api from '@pando/client/services/api'
import { Badge, Button, Input, Spinner } from '@/components/ui'
import { CircleCheck, Download, ExternalLink, Play, RefreshCw, SquareTerminal } from '@/components/ui/icons'
import { openExternal } from '@/services/desktopRuntime'
import { CommandLine, Notice } from './setupShared'
import { apiErrorMessage } from './setupUtils'

interface InstallOption {
  id: string
  label: string
  command?: string
  url?: string
  recommended: boolean
  runnable: boolean
  reason?: string
}

interface OllamaStatus {
  os: string
  baseUrl: string
  installed: boolean
  binaryPath?: string
  running: boolean
  version?: string
  models: string[]
  installOptions: InstallOption[]
  canStart: boolean
  documentModel: string
  codeModel: string
  hasDocumentModel: boolean
  hasCodeModel: boolean
  remembrancesEnabled: boolean
}

interface Job {
  id: string
  kind: string
  target: string
  state: 'running' | 'done' | 'error'
  status?: string
  completed: number
  total: number
  output?: string[]
  error?: string
}

function hasModel(models: string[], name: string): boolean {
  const n = name.toLowerCase()
  return models.some((m) => m.toLowerCase() === n || m.toLowerCase() === `${n}:latest`)
}

function formatBytes(n: number): string {
  if (n >= 1e9) return `${(n / 1e9).toFixed(1)} GB`
  if (n >= 1e6) return `${(n / 1e6).toFixed(0)} MB`
  return `${Math.round(n / 1e3)} KB`
}

/** Polls a setup job until it finishes. */
function useJob(onFinished: (job: Job) => void) {
  const [job, setJob] = useState<Job | null>(null)
  const timer = useRef<number | null>(null)
  const finishedRef = useRef(onFinished)
  useEffect(() => {
    finishedRef.current = onFinished
  }, [onFinished])

  const stop = useCallback(() => {
    if (timer.current !== null) {
      window.clearInterval(timer.current)
      timer.current = null
    }
  }, [])

  const track = useCallback(
    (started: Job) => {
      stop()
      setJob(started)
      timer.current = window.setInterval(() => {
        api
          .get<Job>(`/api/v1/setup/jobs/${encodeURIComponent(started.id)}`)
          .then((j) => {
            setJob(j)
            if (j.state !== 'running') {
              stop()
              finishedRef.current(j)
            }
          })
          .catch(() => {})
      }, 1000)
    },
    [stop],
  )

  useEffect(() => stop, [stop])
  return { job, track }
}

function PullRow({
  label,
  hint,
  model,
  onModelChange,
  present,
  canPull,
  onPulled,
}: {
  label: string
  hint: string
  model: string
  onModelChange?: (m: string) => void
  present: boolean
  canPull: boolean
  onPulled: () => void
}) {
  const { t } = useTranslation()
  const [error, setError] = useState<string | null>(null)
  const { job, track } = useJob((j) => {
    if (j.state === 'error') setError(j.error ?? t('setup.remembrances.pullFailed'))
    onPulled()
  })
  const running = job?.state === 'running'
  const pct = job && job.total > 0 ? Math.min(100, Math.round((job.completed / job.total) * 100)) : null

  const pull = async () => {
    setError(null)
    try {
      track(await api.post<Job>('/api/v1/setup/ollama/pull', { model }))
    } catch (e) {
      setError(apiErrorMessage(e, t('setup.remembrances.pullFailed')))
    }
  }

  return (
    <div className="flex flex-col gap-2 rounded-md border border-border bg-card p-3">
      <div className="flex items-center gap-2">
        <span className="text-sm font-medium text-fg">{label}</span>
        {present ? (
          <Badge tone="success" icon={<CircleCheck size={12} />}>{t('setup.remembrances.installed')}</Badge>
        ) : (
          <Badge tone="warning">{t('setup.remembrances.missing')}</Badge>
        )}
      </div>
      <span className="text-xs text-muted">{hint}</span>
      {onModelChange ? (
        <Input size="sm" value={model} onChange={(e) => onModelChange(e.target.value)} className="font-mono" disabled={running} />
      ) : null}
      <CommandLine command={`ollama pull ${model}`} />
      {running && (
        <div className="flex flex-col gap-1">
          <div className="h-1.5 w-full overflow-hidden rounded-pill bg-raised">
            <div className="h-full bg-accent transition-all" style={{ width: `${pct ?? 5}%` }} />
          </div>
          <span className="text-xs text-muted">
            {job?.status}
            {pct !== null && job ? ` — ${pct}% (${formatBytes(job.completed)} / ${formatBytes(job.total)})` : ''}
          </span>
        </div>
      )}
      {error && <Notice tone="danger">{error}</Notice>}
      {!present && (
        <div>
          <Button size="sm" variant="secondary" icon={<Download size={14} />} loading={running} disabled={!canPull || !model.trim()} onClick={() => void pull()}>
            {running ? t('setup.remembrances.pulling') : t('setup.remembrances.pull')}
          </Button>
        </div>
      )}
    </div>
  )
}

interface RemembrancesStepProps {
  onEnabled: (documentModel: string, codeModel: string) => void
}

export default function RemembrancesStep({ onEnabled }: RemembrancesStepProps) {
  const { t } = useTranslation()
  const [status, setStatus] = useState<OllamaStatus | null>(null)
  const [checking, setChecking] = useState(false)
  const [starting, setStarting] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [confirmInstall, setConfirmInstall] = useState<string | null>(null)
  const [docModel, setDocModel] = useState('')
  const [codeModel, setCodeModel] = useState('')
  const [installLog, setInstallLog] = useState<string[]>([])

  const refresh = useCallback(async () => {
    setChecking(true)
    try {
      const s = await api.get<OllamaStatus>('/api/v1/setup/ollama/status')
      setStatus(s)
      setDocModel((m) => m || s.documentModel)
      setCodeModel((m) => m || s.codeModel)
    } catch (e) {
      setError(apiErrorMessage(e, t('setup.remembrances.statusFailed')))
    } finally {
      setChecking(false)
    }
  }, [t])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const install = useJob((j) => {
    setInstallLog(j.output ?? [])
    if (j.state === 'error') setError(j.error ?? t('setup.remembrances.installFailed'))
    void refresh()
  })

  useEffect(() => {
    if (install.job?.output) setInstallLog(install.job.output)
  }, [install.job])

  const runInstall = async (option: InstallOption) => {
    setError(null)
    setConfirmInstall(null)
    try {
      install.track(await api.post<Job>('/api/v1/setup/ollama/install', { option: option.id }))
    } catch (e) {
      setError(apiErrorMessage(e, t('setup.remembrances.installFailed')))
    }
  }

  const start = async () => {
    setStarting(true)
    setError(null)
    try {
      const r = await api.post<{ running: boolean }>('/api/v1/setup/ollama/start', {})
      if (!r.running) setError(t('setup.remembrances.startSlow'))
      await refresh()
    } catch (e) {
      setError(apiErrorMessage(e, t('setup.remembrances.startFailed')))
    } finally {
      setStarting(false)
    }
  }

  const enable = async () => {
    setSaving(true)
    setError(null)
    try {
      const r = await api.post<{ documentModel: string; codeModel: string }>('/api/v1/setup/remembrances', {
        documentModel: docModel.trim(),
        codeModel: codeModel.trim(),
      })
      onEnabled(r.documentModel, r.codeModel)
    } catch (e) {
      setError(apiErrorMessage(e, t('setup.remembrances.enableFailed')))
    } finally {
      setSaving(false)
    }
  }

  if (!status) {
    return (
      <div className="flex justify-center py-10">
        <Spinner label={t('setup.remembrances.checking')} />
      </div>
    )
  }

  const installing = install.job?.state === 'running'
  const docPresent = hasModel(status.models, docModel)
  const codePresent = hasModel(status.models, codeModel)

  return (
    <div className="flex flex-col gap-4">
      <p className="m-0 text-sm text-muted">{t('setup.remembrances.intro')}</p>

      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span className="font-medium text-fg">Ollama</span>
        {status.running ? (
          <Badge tone="success" dot>{t('setup.remembrances.running', { version: status.version ?? '' })}</Badge>
        ) : status.installed ? (
          <Badge tone="warning" dot>{t('setup.remembrances.stopped')}</Badge>
        ) : (
          <Badge tone="danger" dot>{t('setup.remembrances.notInstalled')}</Badge>
        )}
        <span className="font-mono text-xs text-faint">{status.baseUrl}</span>
        <Button size="sm" variant="ghost" icon={<RefreshCw size={14} />} loading={checking} onClick={() => void refresh()}>
          {t('setup.remembrances.checkAgain')}
        </Button>
      </div>

      {/* 1. Install */}
      {!status.installed && (
        <div className="flex flex-col gap-3">
          <span className="text-sm font-medium text-fg">{t('setup.remembrances.installTitle')}</span>
          {status.installOptions.map((option) => (
            <div key={option.id} className="flex flex-col gap-2 rounded-md border border-border bg-card p-3">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium text-fg">{t(`setup.remembrances.option.${option.id}`, { defaultValue: option.label })}</span>
                {option.recommended && <Badge tone="accent">{t('setup.remembrances.recommended')}</Badge>}
              </div>
              <span className="text-xs text-muted">{t(`setup.remembrances.optionHint.${option.id}`, { defaultValue: '' })}</span>
              {option.command && <CommandLine command={option.command} />}
              <div className="flex flex-wrap items-center gap-2">
                {option.url && (
                  <Button size="sm" variant={option.recommended ? 'primary' : 'secondary'} icon={<ExternalLink size={14} />} onClick={() => void openExternal(option.url!)}>
                    {t('setup.remembrances.openDownload')}
                  </Button>
                )}
                {option.command && option.runnable && confirmInstall !== option.id && (
                  <Button size="sm" variant={option.recommended ? 'primary' : 'secondary'} icon={<Play size={14} />} disabled={installing} onClick={() => setConfirmInstall(option.id)}>
                    {t('setup.remembrances.runForMe')}
                  </Button>
                )}
                {confirmInstall === option.id && (
                  <>
                    <span className="text-xs text-muted">{t('setup.remembrances.confirmRun')}</span>
                    <Button size="sm" variant="primary" onClick={() => void runInstall(option)}>
                      {t('setup.remembrances.confirm')}
                    </Button>
                    <Button size="sm" variant="ghost" onClick={() => setConfirmInstall(null)}>
                      {t('setup.common.cancel')}
                    </Button>
                  </>
                )}
                {option.command && !option.runnable && option.reason && (
                  <span className="text-xs text-faint">{option.reason}</span>
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      {(installing || installLog.length > 0) && (
        <div className="flex flex-col gap-1">
          <span className="flex items-center gap-2 text-xs text-muted">
            <SquareTerminal size={14} />
            {installing ? t('setup.remembrances.installing') : t('setup.remembrances.installOutput')}
          </span>
          <pre className="m-0 max-h-40 overflow-auto rounded-sm bg-shell p-2 font-mono text-xs text-muted">{installLog.slice(-40).join('\n')}</pre>
        </div>
      )}

      {/* 2. Start */}
      {status.installed && !status.running && (
        <Notice tone="warning">
          <div className="flex flex-wrap items-center gap-2">
            <span>{t('setup.remembrances.notRunning')}</span>
            {status.canStart && (
              <Button size="sm" variant="primary" icon={<Play size={14} />} loading={starting} onClick={() => void start()}>
                {t('setup.remembrances.start')}
              </Button>
            )}
          </div>
          <CommandLine command="ollama serve" className="mt-2" />
        </Notice>
      )}

      {/* 3. Pull the embedding models */}
      {(status.installed || status.running) && (
        <div className="flex flex-col gap-3">
          <span className="text-sm font-medium text-fg">{t('setup.remembrances.modelsTitle')}</span>
          <PullRow
            label={t('setup.remembrances.docModel')}
            hint={t('setup.remembrances.docModelHint')}
            model={docModel}
            onModelChange={setDocModel}
            present={docPresent}
            canPull={status.running}
            onPulled={() => void refresh()}
          />
          <PullRow
            label={t('setup.remembrances.codeModel')}
            hint={t('setup.remembrances.codeModelHint')}
            model={codeModel}
            onModelChange={setCodeModel}
            present={codePresent}
            canPull={status.running}
            onPulled={() => void refresh()}
          />
        </div>
      )}

      {error && <Notice tone="danger">{error}</Notice>}

      {status.running && (!docPresent || !codePresent) && (
        <Notice tone="info">{t('setup.remembrances.pullFirst')}</Notice>
      )}

      <div>
        <Button variant="primary" loading={saving} disabled={!status.running || !docModel.trim()} onClick={() => void enable()}>
          {t('setup.remembrances.enable')}
        </Button>
      </div>
    </div>
  )
}
