import { useTranslation } from 'react-i18next'
import { Button, Dialog } from '@/components/ui'
import type { Project } from '@pando/client/types'

interface ProjectInitWizardProps {
  project: Project
  onConfirm: () => Promise<void>
  onCancel: () => void
}

function shortenPath(path: string): string {
  return path.replace(/^\/home\/[^/]+/, '~').replace(/^\/Users\/[^/]+/, '~')
}

export default function ProjectInitWizard({ project, onConfirm, onCancel }: ProjectInitWizardProps) {
  const { t } = useTranslation()

  return (
    <Dialog
      open
      onClose={onCancel}
      title={t('projects.initWizard.title')}
      size="sm"
      closeLabel={t('projects.initWizard.cancel')}
      footer={
        <>
          <Button variant="secondary" onClick={onCancel}>
            {t('projects.initWizard.cancel')}
          </Button>
          <Button variant="primary" onClick={() => void onConfirm()} data-autofocus>
            {t('projects.initWizard.confirm')}
          </Button>
        </>
      }
    >
      <div className="mb-4 rounded-sm bg-shell px-3 py-2.5 font-mono text-sm text-fg">
        {shortenPath(project.path)}
      </div>

      <p className="mb-3 text-sm text-muted">{t('projects.initWizard.description')}</p>

      <ul className="m-0 flex list-disc flex-col gap-1.5 pl-5 text-sm text-muted">
        <li><code className="text-fg">.pando.toml</code> — {t('projects.initWizard.items.config')}</li>
        <li><code className="text-fg">.pando/data/</code> — {t('projects.initWizard.items.database')}</li>
        <li><code className="text-fg">.pando/mesnada/</code> — {t('projects.initWizard.items.agents')}</li>
        <li><code className="text-fg">agents/skills/</code> — {t('projects.initWizard.items.skills')}</li>
      </ul>
    </Dialog>
  )
}
