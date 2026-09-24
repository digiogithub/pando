import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { TextInput } from '@/components/shared/FormInput'
import { authenticateWithCredentials } from '@pando/client/services/auth'
import { Button, Card } from '@/components/ui'
import '@/styles/auth.css'

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
    <div className="auth-shell">
      <Card elevated padding="lg" className="auth-card">
        <form onSubmit={handleSubmit} className="auth-form">
          <div>
            <h3 className="auth-title">{t('login.title')}</h3>
            <p className="auth-description">{t('login.description')}</p>
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

          {error && <div className="auth-error">{error}</div>}

          <Button type="submit" variant="primary" block loading={submitting} disabled={!username || !password}>
            {submitting ? t('login.signingIn') : t('login.signIn')}
          </Button>
        </form>
      </Card>
    </div>
  )
}
