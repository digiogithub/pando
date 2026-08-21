import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { TextInput } from '@/components/shared/FormInput'
import { authenticateWithCredentials } from '@pando/client/services/auth'

interface LoginDialogProps {
  onSuccess: () => void
}

export default function LoginDialog({ onSuccess }: LoginDialogProps) {
  const { t } = useTranslation()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!username || !password) return
    setSubmitting(true)
    setError('')
    try {
      await authenticateWithCredentials(username, password)
      onSuccess()
    } catch {
      setError(t('login.invalid'))
      setPassword('')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div
      style={{
        position: 'fixed',
        inset: 0,
        background: 'var(--bg)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1000,
      }}
    >
      <form
        onSubmit={handleSubmit}
        style={{
          background: 'var(--card-bg)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius-lg)',
          padding: '1.5rem',
          width: 360,
          maxWidth: '90vw',
          boxShadow: '0 8px 32px rgba(0,0,0,0.4)',
          display: 'flex',
          flexDirection: 'column',
          gap: '1rem',
        }}
      >
        <div>
          <h3 style={{ fontSize: 16, fontWeight: 700, color: 'var(--fg)', marginBottom: '0.5rem' }}>
            {t('login.title')}
          </h3>
          <p style={{ fontSize: 14, color: 'var(--fg-muted)', lineHeight: 1.5 }}>{t('login.description')}</p>
        </div>

        <TextInput
          label={t('login.username')}
          value={username}
          autoFocus
          autoComplete="username"
          onChange={(e) => setUsername(e.target.value)}
        />
        <TextInput
          label={t('login.password')}
          type="password"
          value={password}
          autoComplete="current-password"
          onChange={(e) => setPassword(e.target.value)}
        />

        {error && (
          <div
            style={{
              padding: '0.5rem 0.75rem',
              background: 'var(--error)',
              color: 'var(--primary-fg)',
              borderRadius: 'var(--radius-sm)',
              fontSize: 13,
            }}
          >
            {error}
          </div>
        )}

        <button
          type="submit"
          disabled={submitting || !username || !password}
          style={{
            padding: '0.5rem 1.5rem',
            background: submitting || !username || !password ? 'var(--border)' : 'var(--primary)',
            color: submitting || !username || !password ? 'var(--fg-muted)' : 'var(--primary-fg)',
            border: 'none',
            borderRadius: 'var(--radius-sm)',
            fontSize: 14,
            fontWeight: 600,
            cursor: submitting || !username || !password ? 'not-allowed' : 'pointer',
            fontFamily: 'inherit',
          }}
        >
          {submitting ? t('login.signingIn') : t('login.signIn')}
        </button>
      </form>
    </div>
  )
}
