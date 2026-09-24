import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useConfigInitStore } from '@pando/client/stores/configInitStore'
import { Button } from '@/components/ui'
import { Info } from '@/components/ui/icons'
import '@/styles/overlays.css'

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
    <div className="ovl-banner">
      <span className="ovl-banner-text">
        <Info size={14} />
        {status.hasPandoDir
          ? 'Project is pre-initialised but has no local config file.'
          : 'No .pando.toml found in current directory.'}
        {' '}Generate one to configure providers and models.
      </span>

      <div className="ovl-banner-actions">
        <Button size="sm" variant="secondary" loading={generating} onClick={handleGenerate}>
          {generating ? 'Generating…' : 'Generate .pando.toml'}
        </Button>
        <Button size="sm" variant="ghost" onClick={handleDismiss}>
          Dismiss
        </Button>
      </div>
    </div>
  )
}
