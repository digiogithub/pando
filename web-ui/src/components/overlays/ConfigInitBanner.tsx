import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useConfigInitStore } from '@/stores/configInitStore'

/**
 * ConfigInitBanner — shown at the top of the layout when no local .pando.toml
 * exists in the working directory. Offers to generate the file and then
 * navigates to the Settings page so the user can configure providers and models.
 */
export default function ConfigInitBanner() {
  const { status, loading, generating, dismissed, fetchStatus, generateConfig, dismiss } =
    useConfigInitStore()
  const navigate = useNavigate()

  useEffect(() => {
    void fetchStatus()
  }, [fetchStatus])

  // Don't render if still loading, dismissed, or config already present.
  if (loading || dismissed || !status || !status.shouldGenerate) return null

  const handleGenerate = async () => {
    const ok = await generateConfig()
    if (ok) {
      navigate('/settings')
    }
  }

  const handleDismiss = () => {
    dismiss()
  }

  return (
    <div
      style={{
        background: 'var(--primary)',
        color: 'var(--bg)',
        padding: '8px 16px',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        gap: 12,
        fontSize: 13,
        fontWeight: 500,
        zIndex: 200,
        flexShrink: 0,
      }}
    >
      <span>
        {status.hasPandoDir
          ? 'Project is pre-initialised but has no local config file.'
          : 'No .pando.toml found in current directory.'}
        {' '}Generate one to configure providers and models.
      </span>

      <div style={{ display: 'flex', gap: 8, flexShrink: 0 }}>
        <button
          onClick={handleGenerate}
          disabled={generating}
          style={{
            background: 'var(--bg)',
            color: 'var(--primary)',
            border: 'none',
            borderRadius: 4,
            padding: '4px 12px',
            cursor: generating ? 'not-allowed' : 'pointer',
            fontWeight: 600,
            fontSize: 12,
          }}
        >
          {generating ? 'Generating…' : 'Generate .pando.toml'}
        </button>

        <button
          onClick={handleDismiss}
          style={{
            background: 'transparent',
            color: 'var(--bg)',
            border: '1px solid var(--bg)',
            borderRadius: 4,
            padding: '4px 10px',
            cursor: 'pointer',
            fontSize: 12,
          }}
        >
          Dismiss
        </button>
      </div>
    </div>
  )
}
