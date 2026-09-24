import { TriangleAlert } from '@/components/ui/icons'

export default function RestartRequiredBanner() {
  return (
    <div className="banner banner--soft-warning mb-5">
      <TriangleAlert size={15} />
      <span>Changes to this section require restarting the application to take effect.</span>
    </div>
  )
}
