import { Link } from 'react-router-dom'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faArrowLeft } from '@fortawesome/free-solid-svg-icons'

export default function NotFound() {
  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        height: '100%',
        gap: '1rem',
        color: 'var(--fg)',
        padding: '2rem',
      }}
    >
      <div style={{ fontSize: 64, lineHeight: 1, marginBottom: '0.5rem', opacity: 0.3 }}>
        404
      </div>
      <h2 style={{ fontSize: 20, fontWeight: 600, margin: 0 }}>Page not found</h2>
      <p style={{ fontSize: 14, color: 'var(--fg-muted)', margin: 0, textAlign: 'center' }}>
        The page you are looking for does not exist or has been moved.
      </p>
      <Link
        to="/chat"
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: '0.5rem',
          padding: '0.5rem 1.25rem',
          marginTop: '0.5rem',
          background: 'var(--primary)',
          color: 'var(--primary-fg)',
          borderRadius: 'var(--radius-sm)',
          textDecoration: 'none',
          fontSize: 13,
          fontWeight: 600,
        }}
      >
        <FontAwesomeIcon icon={faArrowLeft} />
        Back to Chat
      </Link>
    </div>
  )
}
