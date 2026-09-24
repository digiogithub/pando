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
  return (
    <Dialog
      open
      onClose={onCancel}
      title="Initialize Project"
      size="sm"
      footer={
        <>
          <Button variant="secondary" onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void onConfirm()} data-autofocus>
            Initialize &amp; Open
          </Button>
        </>
      }
    >
      <div className="mb-4 rounded-sm bg-shell px-3 py-2.5 font-mono text-sm text-fg">
        {shortenPath(project.path)}
      </div>

      <p className="mb-3 text-sm text-muted">No Pando config found at this path. The following will be created:</p>

      <ul className="m-0 flex list-disc flex-col gap-1.5 pl-5 text-sm text-muted">
        <li><code className="text-fg">.pando.toml</code> — configuration</li>
        <li><code className="text-fg">.pando/data/</code> — database</li>
        <li><code className="text-fg">.pando/mesnada/</code> — agents &amp; personas</li>
        <li><code className="text-fg">agents/skills/</code> — custom skills</li>
      </ul>
    </Dialog>
  )
}
