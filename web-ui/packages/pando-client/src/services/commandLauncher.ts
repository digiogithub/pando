import api from '../services/api'
import { useToastStore } from '../stores/toastStore'
import type { ProviderTypeInfo } from '../types'

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
  enterpriseUrl?: string
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

function formatCopilotStatus(status: AuthProviderStatusResponse): string {
  const bits = [status.enterpriseUrl, status.source]
    .map((value) => value?.trim())
    .filter(Boolean)
  return bits.length > 0 ? `${status.message} (${bits.join(' · ')})` : status.message
}

async function fetchAuthProviderTypes(): Promise<AuthProviderMeta[]> {
  const data = await api.get<{ providerTypes?: ProviderTypeInfo[] }>('/api/v1/config/provider-types')
  return (data.providerTypes ?? [])
    .filter((provider) => provider.type === 'copilot')
    .map((provider) => ({
      type: provider.type,
      displayName: provider.displayName,
      supportsOAuth: provider.supportsOAuth,
    }))
}

function buildProviderKeywords(provider: AuthProviderMeta): string[] {
  const base = [provider.type, provider.displayName.toLowerCase()]
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
        const message = provider.type === 'copilot' ? formatCopilotStatus(status) : status.message
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
          // Show a persistent toast (ttlMs=0) so the device code stays visible
          // until the user manually dismisses it — the code is needed to complete
          // the authorization at github.com/login/device.
          useToastStore.getState().addToast(
            `GitHub Copilot — Enter code: ${result.userCode} at ${result.verificationUri}`,
            'info',
            0,
          )
        },
      })
    }
  }

  return commands
}
