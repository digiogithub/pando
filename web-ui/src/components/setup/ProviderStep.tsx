import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import api from '@pando/client/services/api'
import type { ProviderAccount } from '@pando/client/types'
import { Button, Input, Spinner } from '@/components/ui'
import {
  Bot,
  Brain,
  Check,
  Code,
  Copy,
  ExternalLink,
  Network,
  Package,
  Plug,
  Rocket,
  Sparkles,
  Zap,
  type LucideIcon,
} from '@/components/ui/icons'
import { openExternal } from '@/services/desktopRuntime'
import { ChoiceCard, Notice } from './setupShared'
import { apiErrorMessage, copyText } from './setupUtils'

interface ProviderGuide {
  type: string
  label: string
  icon: LucideIcon
  /** Where the user gets an API key. */
  keyUrl?: string
  needsKey: boolean
  needsBaseUrl: boolean
  defaultBaseUrl?: string
}

// The providers the assistant walks through. Azure, Bedrock and Vertex AI need
// cloud credentials beyond a key and stay in Settings > Providers.
const PROVIDERS: ProviderGuide[] = [
  { type: 'copilot', label: 'GitHub Copilot', icon: Code, needsKey: false, needsBaseUrl: false },
  { type: 'anthropic', label: 'Anthropic', icon: Bot, keyUrl: 'https://console.anthropic.com/settings/keys', needsKey: true, needsBaseUrl: false },
  { type: 'openai', label: 'OpenAI', icon: Brain, keyUrl: 'https://platform.openai.com/api-keys', needsKey: true, needsBaseUrl: false },
  { type: 'gemini', label: 'Google Gemini', icon: Sparkles, keyUrl: 'https://aistudio.google.com/app/apikey', needsKey: true, needsBaseUrl: false },
  { type: 'openrouter', label: 'OpenRouter', icon: Network, keyUrl: 'https://openrouter.ai/keys', needsKey: true, needsBaseUrl: false },
  { type: 'groq', label: 'Groq', icon: Zap, keyUrl: 'https://console.groq.com/keys', needsKey: true, needsBaseUrl: false },
  { type: 'xai', label: 'xAI (Grok)', icon: Rocket, keyUrl: 'https://console.x.ai', needsKey: true, needsBaseUrl: false },
  { type: 'ollama', label: 'Ollama', icon: Package, needsKey: false, needsBaseUrl: true, defaultBaseUrl: 'http://localhost:11434' },
  { type: 'openai-compatible', label: 'OpenAI Compatible', icon: Plug, needsKey: true, needsBaseUrl: true },
]

interface CopilotCode {
  verificationUri: string
  userCode: string
  expiresIn: number
  interval: number
}

interface ProviderStepProps {
  /** Called with the provider type once an account is configured and authenticated. */
  onConfigured: (providerType: string) => void
  /** Called when the user keeps the accounts that already exist. */
  onKeepExisting: (providerType: string | null) => void
  /** Lets the parent enable its Next button (false while the Copilot code is pending). */
  setBusy: (busy: boolean) => void
}

function uniqueAccountId(type: string, existing: ProviderAccount[]): string {
  const ids = new Set(existing.map((a) => a.id))
  if (!ids.has(type)) return type
  for (let i = 2; ; i++) {
    const id = `${type}-${i}`
    if (!ids.has(id)) return id
  }
}

export default function ProviderStep({ onConfigured, onKeepExisting, setBusy }: ProviderStepProps) {
  const { t } = useTranslation()
  const [accounts, setAccounts] = useState<ProviderAccount[] | null>(null)
  const [addingNew, setAddingNew] = useState(false)
  const [selected, setSelected] = useState<string>('copilot')
  const [apiKey, setApiKey] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [copilot, setCopilot] = useState<CopilotCode | null>(null)
  const [copied, setCopied] = useState(false)
  const pollRef = useRef<number | null>(null)

  const guide = PROVIDERS.find((p) => p.type === selected) ?? PROVIDERS[0]

  useEffect(() => {
    api
      .get<{ providerAccounts: ProviderAccount[] }>('/api/v1/config/provider-accounts')
      .then((r) => setAccounts(r.providerAccounts ?? []))
      .catch(() => setAccounts([]))
  }, [])

  useEffect(() => {
    setBaseUrl(guide.defaultBaseUrl ?? '')
    setApiKey('')
    setError(null)
  }, [guide])

  const stopPolling = useCallback(() => {
    if (pollRef.current !== null) {
      window.clearInterval(pollRef.current)
      pollRef.current = null
    }
  }, [])

  useEffect(() => stopPolling, [stopPolling])

  useEffect(() => {
    setBusy(saving || copilot !== null)
  }, [saving, copilot, setBusy])

  const copilotAuthenticated = async (): Promise<boolean> => {
    try {
      const status = await api.get<{ authenticated: boolean }>('/api/v1/auth/providers/copilot/status')
      return status.authenticated
    } catch {
      return false
    }
  }

  const startCopilotLogin = async () => {
    setError(null)
    try {
      const code = await api.post<CopilotCode>('/api/v1/auth/providers/copilot/login', {})
      setCopilot(code)
      stopPolling()
      const everyMs = Math.max(code.interval || 5, 3) * 1000
      const deadline = Date.now() + (code.expiresIn || 900) * 1000
      pollRef.current = window.setInterval(() => {
        if (Date.now() > deadline) {
          stopPolling()
          setCopilot(null)
          setError(t('setup.provider.copilotExpired'))
          return
        }
        void copilotAuthenticated().then((ok) => {
          if (!ok) return
          stopPolling()
          setCopilot(null)
          onConfigured('copilot')
        })
      }, everyMs)
    } catch (e) {
      setError(apiErrorMessage(e, t('setup.provider.copilotStartFailed')))
    }
  }

  const handleSave = async () => {
    if (guide.needsKey && !apiKey.trim()) {
      setError(t('setup.provider.keyRequired'))
      return
    }
    if (guide.needsBaseUrl && !baseUrl.trim()) {
      setError(t('setup.provider.baseUrlRequired'))
      return
    }
    setSaving(true)
    setError(null)
    try {
      const existing = accounts ?? []
      const reuse = guide.type === 'copilot' ? existing.find((a) => a.type === 'copilot') : undefined
      if (!reuse) {
        await api.post('/api/v1/config/provider-accounts', {
          id: uniqueAccountId(guide.type, existing),
          displayName: guide.label,
          type: guide.type,
          ...(guide.needsKey ? { apiKey: apiKey.trim() } : {}),
          ...(guide.needsBaseUrl ? { baseUrl: baseUrl.trim() } : {}),
        })
      }
      if (guide.type === 'copilot') {
        // Editor-issued GitHub tokens are picked up without a new login.
        if (await copilotAuthenticated()) {
          onConfigured('copilot')
        } else {
          await startCopilotLogin()
        }
        return
      }
      onConfigured(guide.type)
    } catch (e) {
      setError(apiErrorMessage(e, t('setup.provider.saveFailed')))
    } finally {
      setSaving(false)
    }
  }

  if (accounts === null) {
    return (
      <div className="flex justify-center py-10">
        <Spinner label={t('setup.common.loading')} />
      </div>
    )
  }

  // Copilot device flow: the code takes the centre of the assistant.
  if (copilot) {
    return (
      <div className="flex flex-col items-center gap-5 py-4 text-center">
        <p className="m-0 max-w-md text-sm text-muted">{t('setup.provider.copilotIntro')}</p>
        <ol className="m-0 flex max-w-md list-decimal flex-col gap-1 pl-5 text-left text-sm text-muted">
          <li>{t('setup.provider.copilotStep1')}</li>
          <li>{t('setup.provider.copilotStep2')}</li>
          <li>{t('setup.provider.copilotStep3')}</li>
        </ol>
        <div
          className="select-all rounded-lg border border-border-strong bg-shell px-8 py-5 font-mono text-4xl font-semibold tracking-[0.3em] text-fg"
          aria-label={t('setup.provider.copilotCodeLabel')}
        >
          {copilot.userCode}
        </div>
        <div className="flex flex-wrap justify-center gap-2">
          <Button
            variant="secondary"
            icon={copied ? <Check size={14} /> : <Copy size={14} />}
            onClick={() => {
              void copyText(copilot.userCode).then((ok) => {
                if (!ok) return
                setCopied(true)
                window.setTimeout(() => setCopied(false), 1500)
              })
            }}
          >
            {copied ? t('setup.common.copied') : t('setup.provider.copyCode')}
          </Button>
          <Button variant="primary" icon={<ExternalLink size={14} />} onClick={() => void openExternal(copilot.verificationUri)}>
            {t('setup.provider.openGithub')}
          </Button>
        </div>
        <a className="font-mono text-xs text-muted" href={copilot.verificationUri} onClick={(e) => { e.preventDefault(); void openExternal(copilot.verificationUri) }}>
          {copilot.verificationUri}
        </a>
        <div className="flex items-center gap-2 text-sm text-muted">
          <Spinner size={14} />
          {t('setup.provider.copilotWaiting')}
        </div>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            stopPolling()
            setCopilot(null)
          }}
        >
          {t('setup.provider.copilotCancel')}
        </Button>
      </div>
    )
  }

  if (accounts.length > 0 && !addingNew) {
    const firstType = accounts.find((a) => !a.disabled)?.type ?? null
    return (
      <div className="flex flex-col gap-4">
        <Notice tone="success">{t('setup.provider.existing', { count: accounts.length })}</Notice>
        <ul className="m-0 flex list-none flex-col gap-1.5 p-0">
          {accounts.map((a) => (
            <li key={a.id} className="flex items-center gap-2 rounded-sm border border-border bg-card px-3 py-2 text-sm">
              <span className="font-medium text-fg">{a.displayName || a.id}</span>
              <span className="font-mono text-xs text-muted">{a.type}</span>
              {a.disabled && <span className="text-xs text-faint">{t('setup.provider.disabled')}</span>}
            </li>
          ))}
        </ul>
        <div className="flex flex-wrap gap-2">
          <Button variant="primary" onClick={() => onKeepExisting(firstType)}>
            {t('setup.provider.keepExisting')}
          </Button>
          <Button variant="secondary" onClick={() => setAddingNew(true)}>
            {t('setup.provider.addAnother')}
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      <p className="m-0 text-sm text-muted">{t('setup.provider.intro')}</p>
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
        {PROVIDERS.map((p) => {
          const Icon = p.icon
          return (
            <ChoiceCard key={p.type} selected={selected === p.type} onClick={() => setSelected(p.type)} icon={<Icon size={16} />} title={p.label} />
          )
        })}
      </div>

      <div className="flex flex-col gap-3 rounded-md border border-border bg-card p-4">
        <p className="m-0 text-sm text-fg">{t(`setup.provider.guide.${guide.type}`)}</p>

        {guide.keyUrl && (
          <div>
            <Button variant="secondary" size="sm" icon={<ExternalLink size={14} />} onClick={() => void openExternal(guide.keyUrl!)}>
              {t('setup.provider.getKey')}
            </Button>
          </div>
        )}

        {guide.needsBaseUrl && (
          <label className="flex flex-col gap-1 text-sm text-muted">
            {t('setup.provider.baseUrl')}
            <Input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} placeholder={guide.defaultBaseUrl ?? 'https://api.example.com/v1'} />
          </label>
        )}

        {guide.needsKey && (
          <label className="flex flex-col gap-1 text-sm text-muted">
            {t('setup.provider.apiKey')}
            <Input type="password" autoComplete="off" value={apiKey} onChange={(e) => setApiKey(e.target.value)} placeholder="sk-…" />
          </label>
        )}

        {error && <Notice tone="danger">{error}</Notice>}

        <div>
          <Button variant="primary" loading={saving} onClick={() => void handleSave()}>
            {guide.type === 'copilot' ? t('setup.provider.connectCopilot') : t('setup.provider.save')}
          </Button>
        </div>
      </div>

      <p className="m-0 text-xs text-faint">{t('setup.provider.moreInSettings')}</p>
    </div>
  )
}
