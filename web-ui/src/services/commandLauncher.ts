import api from '@/services/api'
import type { ProviderTypeInfo } from '@/types'

export interface LauncherCommand {
  id: string
  label: string
  description: string
  group: 'view' | 'command' | 'recent' | 'account'
  path?: string
  keywords?: string[]
  action?: () => Promise<void> | void
}

interface AuthProviderStatusResponse {
  provider: string
  authenticated: boolean
  source?: string
  message: string
  displayName?: string
  email?: string
  subscriptionType?: string
  enterpriseUrl?: string
}

interface ClaudeLoginStartResponse {
  manualUrl: string
  autoUrl: string
  message: string
}

interface ClaudeLoginCompleteResponse {
  authenticated: boolean
  source?: string
  displayName?: string
  email?: string
  subscriptionType?: string
  message: string
}

interface CopilotLoginStartResponse {
  verificationUri: string
  userCode: string
  expiresIn?: number
  interval?: number
  enterpriseUrl?: string
  message: string
}

interface AuthProviderMeta {
  type: string
  displayName: string
  supportsOAuth: boolean
}

function formatClaudeStatus(status: AuthProviderStatusResponse): string {
  const bits = [status.displayName, status.email, status.subscriptionType, status.source]
    .map((value) => value?.trim())
    .filter(Boolean)
  return bits.length > 0 ? `${status.message} (${bits.join(' · ')})` : status.message
}

function formatCopilotStatus(status: AuthProviderStatusResponse): string {
  const bits = [status.enterpriseUrl, status.source]
    .map((value) => value?.trim())
    .filter(Boolean)
  return bits.length > 0 ? `${status.message} (${bits.join(' · ')})` : status.message
}

async function fetchAuthProviderTypes(): Promise<AuthProviderMeta[]> {
  const data = await api.get<{ providerTypes?: ProviderTypeInfo[] }>('/api/v1/config/provider-types')
  return (data.providerTypes ?? [])
    .filter((provider) => provider.supportsOAuth || provider.type === 'anthropic' || provider.type === 'copilot')
    .map((provider) => ({
      type: provider.type,
      displayName: provider.displayName,
      supportsOAuth: provider.supportsOAuth,
    }))
}

function buildProviderKeywords(provider: AuthProviderMeta): string[] {
  const base = [provider.type, provider.displayName.toLowerCase()]
  if (provider.type === 'anthropic') {
    base.push('claude', 'oauth')
  }
  if (provider.type === 'copilot') {
    base.push('github', 'oauth')
  }
  return base
}

export async function loadLauncherCommands(notify: (message: string, type?: 'success' | 'error' | 'warning' | 'info') => void): Promise<LauncherCommand[]> {
  const providers = await fetchAuthProviderTypes()
  const commands: LauncherCommand[] = []

  for (const provider of providers) {
    const keywords = buildProviderKeywords(provider)

    commands.push({
      id: `${provider.type}:status`,
      label: `${provider.displayName} Status`,
      description: `Show ${provider.displayName} authentication status`,
      group: 'account',
      keywords: [...keywords, 'status'],
      action: async () => {
        const status = await api.get<AuthProviderStatusResponse>(`/api/v1/auth/providers/${provider.type}/status`)
        const message = provider.type === 'anthropic' ? formatClaudeStatus(status) : provider.type === 'copilot' ? formatCopilotStatus(status) : status.message
        notify(message, status.authenticated ? 'success' : 'warning')
      },
    })

    commands.push({
      id: `${provider.type}:logout`,
      label: `${provider.displayName} Logout`,
      description: `Remove saved ${provider.displayName} credentials`,
      group: 'account',
      keywords: [...keywords, 'logout', 'sign out'],
      action: async () => {
        const result = await api.post<{ message: string }>(`/api/v1/auth/providers/${provider.type}/logout`, {})
        notify(result.message, 'success')
      },
    })

    if (provider.type === 'anthropic') {
      commands.push(
        {
          id: 'anthropic:login',
          label: 'Anthropic Login',
          description: 'Start Claude.ai OAuth login in your browser',
          group: 'account',
          keywords: [...keywords, 'login', 'sign in'],
          action: async () => {
            const result = await api.post<ClaudeLoginStartResponse>('/api/v1/auth/providers/anthropic/login', {})
            window.open(result.autoUrl || result.manualUrl, '_blank', 'noopener,noreferrer')
            notify(result.message, 'info')
          },
        },
        {
          id: 'anthropic:usage',
          label: 'Anthropic Usage',
          description: 'Show Claude usage statistics if available',
          group: 'account',
          keywords: [...keywords, 'usage', 'stats', 'tokens'],
          action: async () => {
            const result = await api.get<{ content: string }>('/api/v1/auth/providers/anthropic/stats')
            notify(result.content, 'info')
          },
        },
        {
          id: 'anthropic:complete-login',
          label: 'Anthropic Complete Login',
          description: 'Paste the Claude authorization code or callback URL to finish login',
          group: 'account',
          keywords: [...keywords, 'complete', 'code', 'callback'],
          action: async () => {
            const raw = window.prompt('Paste the Claude authorization code or callback URL')?.trim()
            if (!raw) return
            const result = await api.post<ClaudeLoginCompleteResponse>('/api/v1/auth/providers/anthropic/login/complete', { input: raw })
            notify(result.message, result.authenticated ? 'success' : 'warning')
          },
        },
      )
      continue
    }

    if (provider.type === 'copilot') {
      commands.push({
        id: 'copilot:login',
        label: 'Copilot Login',
        description: 'Start GitHub Copilot device login flow',
        group: 'account',
        keywords: [...keywords, 'login', 'device code', 'sign in'],
        action: async () => {
          const result = await api.post<CopilotLoginStartResponse>('/api/v1/auth/providers/copilot/login', {})
          window.open(result.verificationUri, '_blank', 'noopener,noreferrer')
          notify(`${result.message} Code: ${result.userCode}`, 'info')
        },
      })
    }
  }

  return commands
}
