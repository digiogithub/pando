import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import ConfirmDialog from '@/components/shared/ConfirmDialog'
import { useToastStore } from '@pando/client/stores/toastStore'
import api from '@pando/client/services/api'
import { Button, IconButton, Input, SettingsRow, SettingsSection, Switch } from '@/components/ui'
import { Eye, EyeOff, Trash2 } from '@/components/ui/icons'

interface BasicAuthUser {
  username: string
}

interface BasicAuthStatus {
  enabled: boolean
  users: BasicAuthUser[]
  enforced: boolean
  bindHost: string
}

export default function WebUIAccessSettings() {
  const { t } = useTranslation()
  const [status, setStatus] = useState<BasicAuthStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [newUsername, setNewUsername] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [revealed, setRevealed] = useState<Record<string, string>>({})
  const [pendingDelete, setPendingDelete] = useState<string | null>(null)

  const toast = useToastStore.getState().addToast

  const load = useCallback(async () => {
    try {
      setStatus(await api.get<BasicAuthStatus>('/api/v1/config/api-server/basic-auth'))
    } catch (e) {
      toast(e instanceof Error ? e.message : t('settings.webuiAccess.loadFailed'), 'error')
    } finally {
      setLoading(false)
    }
  }, [t, toast])

  useEffect(() => {
    void load()
  }, [load])

  if (loading || !status) {
    return <div className="settings-loading">Loading…</div>
  }

  async function toggleEnabled(value: boolean) {
    try {
      setStatus(await api.put<BasicAuthStatus>('/api/v1/config/api-server/basic-auth', { enabled: value }))
    } catch (e) {
      toast(e instanceof Error ? e.message : t('settings.webuiAccess.saveFailed'), 'error')
    }
  }

  async function addUser() {
    try {
      setStatus(
        await api.post<BasicAuthStatus>('/api/v1/config/api-server/basic-auth/users', {
          username: newUsername.trim(),
          password: newPassword,
        }),
      )
      setNewUsername('')
      setNewPassword('')
      toast(t('settings.webuiAccess.userSaved'), 'success')
    } catch (e) {
      toast(e instanceof Error ? e.message : t('settings.webuiAccess.saveFailed'), 'error')
    }
  }

  async function deleteUser(username: string) {
    try {
      setStatus(
        await api.delete<BasicAuthStatus>(
          `/api/v1/config/api-server/basic-auth/users/${encodeURIComponent(username)}`,
        ),
      )
      setRevealed((prev) => {
        const next = { ...prev }
        delete next[username]
        return next
      })
    } catch (e) {
      toast(e instanceof Error ? e.message : t('settings.webuiAccess.deleteFailed'), 'error')
    } finally {
      setPendingDelete(null)
    }
  }

  async function revealPassword(username: string) {
    if (revealed[username]) {
      setRevealed((prev) => {
        const next = { ...prev }
        delete next[username]
        return next
      })
      return
    }
    try {
      const data = await api.post<{ password: string }>(
        `/api/v1/config/api-server/basic-auth/users/${encodeURIComponent(username)}/reveal`,
        {},
      )
      setRevealed((prev) => ({ ...prev, [username]: data.password }))
    } catch (e) {
      toast(e instanceof Error ? e.message : t('settings.webuiAccess.revealFailed'), 'error')
    }
  }

  return (
    <div>
      <header className="settings-page-header">
        <h2 className="settings-page-title">{t('settings.webuiAccess.title')}</h2>
      </header>

      <div className={`settings-banner ${status.enforced ? 'settings-banner--success' : 'settings-banner--warning'}`}>
        <span>
          {status.enforced
            ? t('settings.webuiAccess.enforced', { host: status.bindHost })
            : t('settings.webuiAccess.notEnforced', { host: status.bindHost || 'localhost' })}
        </span>
      </div>

      <SettingsSection>
        <SettingsRow label={t('settings.webuiAccess.enabled')} description={t('settings.webuiAccess.enabledDescription')} htmlFor="webui-access-enabled">
          <Switch id="webui-access-enabled" checked={status.enabled} onCheckedChange={(v) => void toggleEnabled(v)} />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title={t('settings.webuiAccess.users')}>
        {status.users.length === 0 ? (
          <div className="settings-empty-row">{t('settings.webuiAccess.noUsers')}</div>
        ) : (
          status.users.map((user) => (
            <div className="ui-settings-row" key={user.username}>
              <div className="ui-settings-row-text">
                <span className="ui-settings-row-label">{user.username}</span>
              </div>
              <div className="ui-settings-row-control">
                <span className="settings-code-value">{revealed[user.username] ?? '••••••••'}</span>
                <IconButton
                  aria-label={revealed[user.username] ? t('settings.webuiAccess.hide') : t('settings.webuiAccess.reveal')}
                  tooltip
                  size="sm"
                  icon={revealed[user.username] ? <EyeOff size={14} /> : <Eye size={14} />}
                  onClick={() => void revealPassword(user.username)}
                />
                <IconButton
                  aria-label={t('settings.webuiAccess.delete')}
                  tooltip
                  size="sm"
                  icon={<Trash2 size={14} />}
                  onClick={() => setPendingDelete(user.username)}
                />
              </div>
            </div>
          ))
        )}

        <div className="border-t border-border p-4">
          <div className="settings-field-grid">
            <div className="settings-field">
              <label className="settings-field-label" htmlFor="webui-access-new-username">{t('settings.webuiAccess.username')}</label>
              <Input
                id="webui-access-new-username"
                value={newUsername}
                onChange={(e) => setNewUsername(e.target.value)}
                placeholder="admin"
              />
            </div>
            <div className="settings-field">
              <label className="settings-field-label" htmlFor="webui-access-new-password">{t('settings.webuiAccess.password')}</label>
              <Input
                id="webui-access-new-password"
                type="password"
                value={newPassword}
                onChange={(e) => setNewPassword(e.target.value)}
              />
            </div>
          </div>
          <Button
            className="mt-3"
            variant="primary"
            disabled={!newUsername.trim() || !newPassword}
            onClick={() => void addUser()}
          >
            {t('settings.webuiAccess.addUser')}
          </Button>
          <p className="mt-2 text-xs text-muted">{t('settings.webuiAccess.storageNote')}</p>
        </div>
      </SettingsSection>

      {pendingDelete && (
        <ConfirmDialog
          title={t('settings.webuiAccess.deleteTitle')}
          message={t('settings.webuiAccess.deleteMessage', { username: pendingDelete })}
          confirmLabel={t('settings.webuiAccess.delete')}
          dangerous
          onConfirm={() => void deleteUser(pendingDelete)}
          onCancel={() => setPendingDelete(null)}
        />
      )}
    </div>
  )
}
