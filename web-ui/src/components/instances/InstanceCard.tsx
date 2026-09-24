import { Server, Star } from '@/components/ui/icons'
import { Badge } from '@/components/ui'
import type { InstanceInfo } from '@pando/client/stores/instancesStore'

/** Replace leading /home/<user> or /Users/<user> with ~. */
function shortenPath(path: string): string {
  return path
    .replace(/^\/home\/[^/]+/, '~')
    .replace(/^\/Users\/[^/]+/, '~')
}

const MODE_TONE: Record<string, 'accent' | 'info' | 'success' | 'neutral'> = {
  tui: 'accent',
  webui: 'info',
  desktop: 'accent',
  acp: 'success',
}

interface InstanceCardProps {
  instance: InstanceInfo
  selected: boolean
  onClick: () => void
}

export default function InstanceCard({ instance, selected, onClick }: InstanceCardProps) {
  return (
    <div onClick={onClick} data-selected={selected || undefined} className="entity-row">
      {/* Top row: icon + path + primary badge */}
      <div className="flex items-center gap-2">
        <Server size={13} className={selected ? 'text-accent' : 'text-muted'} />
        <span
          className={`is-ellipsis flex-1 overflow-hidden whitespace-nowrap font-mono text-xs ${selected ? 'font-semibold' : ''} text-fg`}
          title={instance.path}
        >
          {shortenPath(instance.path)}
        </span>
        {instance.is_primary && (
          <Badge tone="warning" icon={<Star size={9} />} className="flex-shrink-0">
            PRIMARY
          </Badge>
        )}
      </div>

      {/* Bottom row: mode badge + PID */}
      <div className="flex items-center gap-2 pl-5">
        <Badge tone={MODE_TONE[instance.mode] ?? 'neutral'} outline className="uppercase tracking-wide">
          {instance.mode}
        </Badge>
        <span className="text-xs text-muted">PID {instance.pid}</span>
        <span className="ml-auto text-[10px] text-faint">{instance.instance_id.slice(0, 8)}</span>
      </div>
    </div>
  )
}
