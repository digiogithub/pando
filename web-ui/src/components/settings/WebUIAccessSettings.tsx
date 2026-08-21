import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { TextInput, Toggle } from '@/components/shared/FormInput'
import ConfirmDialog from '@/components/shared/ConfirmDialog'
import { useToastStore } from '@pando/client/stores/toastStore'
import api from '@pando/client/services/api'

interface BasicAuthUser {
  username: string
}

interface BasicAuthStatus {
  enabled: boolean
  users: BasicAuthUser[]
  enforced: boolean
  bindHost: string
}

const sectionTitle: React.CSSProperties = {
  fontSize: 18,
  fontWeight: 700,
  color: 'var(--fg)',
  marginBottom: '1.25rem',
}

const dividerStyle: React.CSSProperties = {
  borderTop: '1px solid var(--border)',
  margin: '1.5rem 0',
}

const noticeStyle: React.CSSProperties = {
  padding: '0.5rem 0.75rem',
  borderRadius: 'var(--radius-sm)',
  fontSize: 12,
  color: 'var(--fg-muted)',
  marginBottom: '1.25rem',
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
    return <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 14 }}>Loading…</div>
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
    <div style={{ maxWidth: 640 }}>
      <h2 style={sectionTitle}>{t('settings.webuiAccess.title')}</h2>

      <div
        style={{
          ...noticeStyle,
          background: status.enforced ? 'rgba(22, 163, 74, 0.08)' : 'rgba(217, 119, 6, 0.08)',
          border: status.enforced ? '1px solid rgba(22, 163, 74, 0.2)' : '1px solid rgba(217, 119, 6, 0.2)',
        }}
      >
        {status.enforced
          ? t('settings.webuiAccess.enforced', { host: status.bindHost })
          : t('settings.webuiAccess.notEnforced', { host: status.bindHost || 'localhost' })}
      </div>

      <Toggle
        label={t('settings.webuiAccess.enabled')}
        description={t('settings.webuiAccess.enabledDescription')}
        checked={status.enabled}
        onChange={(v) => void toggleEnabled(v)}
      />

      <div style={dividerStyle} />

      <h3 style={{ fontSize: 14, fontWeight: 700, color: 'var(--fg)', marginBottom: '0.75rem' }}>
        {t('settings.webuiAccess.users')}
      </h3>

      {status.users.length === 0 && (
        <p style={{ fontSize: 13, color: 'var(--fg-muted)', marginBottom: '1rem' }}>{t('settings.webuiAccess.noUsers')}</p>
      )}

      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem', marginBottom: '1.25rem' }}>
        {status.users.map((user) => (
          <div
            key={user.username}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '0.75rem',
              padding: '0.5rem 0.75rem',
              border: '1px solid var(--border)',
              borderRadius: 'var(--radius-sm)',
            }}
          >
            <span style={{ flex: 1, fontSize: 14, color: 'var(--fg)' }}>{user.username}</span>
            <span style={{ fontSize: 13, color: 'var(--fg-muted)', fontFamily: 'monospace' }}>
              {revealed[user.username] ?? '••••••••'}
            </span>
            <button
              onClick={() => void revealPassword(user.username)}
              style={{
                padding: '0.25rem 0.625rem',
                background: 'transparent',
                color: 'var(--fg-muted)',
                border: '1px solid var(--border)',
                borderRadius: 'var(--radius-sm)',
                fontSize: 12,
                cursor: 'pointer',
                fontFamily: 'inherit',
              }}
            >
              {revealed[user.username] ? t('settings.webuiAccess.hide') : t('settings.webuiAccess.reveal')}
            </button>
            <button
              onClick={() => setPendingDelete(user.username)}
              style={{
                padding: '0.25rem 0.625rem',
                background: 'transparent',
                color: 'var(--error)',
                border: '1px solid var(--border)',
                borderRadius: 'var(--radius-sm)',
                fontSize: 12,
                cursor: 'pointer',
                fontFamily: 'inherit',
              }}
            >
              {t('settings.webuiAccess.delete')}
            </button>
          </div>
        ))}
      </div>

      <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'flex-end' }}>
        <div style={{ flex: 1 }}>
          <TextInput
            label={t('settings.webuiAccess.username')}
            value={newUsername}
            onChange={(e) => setNewUsername(e.target.value)}
            placeholder="admin"
          />
        </div>
        <div style={{ flex: 1 }}>
          <TextInput
            label={t('settings.webuiAccess.password')}
            type="password"
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
          />
        </div>
        <button
          onClick={() => void addUser()}
          disabled={!newUsername.trim() || !newPassword}
          style={{
            padding: '0.5rem 1.25rem',
            background: !newUsername.trim() || !newPassword ? 'var(--border)' : 'var(--primary)',
            color: !newUsername.trim() || !newPassword ? 'var(--fg-muted)' : 'var(--primary-fg)',
            border: 'none',
            borderRadius: 'var(--radius-sm)',
            fontSize: 14,
            fontWeight: 600,
            cursor: !newUsername.trim() || !newPassword ? 'not-allowed' : 'pointer',
            fontFamily: 'inherit',
          }}
        >
          {t('settings.webuiAccess.addUser')}
        </button>
      </div>

      <p style={{ marginTop: '0.75rem', fontSize: 12, color: 'var(--fg-muted)' }}>
        {t('settings.webuiAccess.storageNote')}
      </p>

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
