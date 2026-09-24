import { Link } from 'react-router-dom'
import { ArrowLeft } from '@/components/ui/icons'

export default function NotFound() {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-4 p-8 text-fg">
      <div className="text-6xl font-semibold leading-none text-faint opacity-60">404</div>
      <h2 className="text-xl font-semibold">Page not found</h2>
      <p className="text-center text-sm text-muted">
        The page you are looking for does not exist or has been moved.
      </p>
      <Link to="/chat" className="ui-btn ui-btn--primary mt-1 no-underline">
        <ArrowLeft size={14} />
        Back to Chat
      </Link>
    </div>
  )
}
