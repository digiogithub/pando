import { Component } from 'react'
import type { ReactNode, ErrorInfo } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faExclamationTriangle, faRotateRight } from '@fortawesome/free-solid-svg-icons'

interface Props {
  children: ReactNode
  fallback?: ReactNode
}

interface State {
  hasError: boolean
  error: Error | null
}

export default class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props)
    this.state = { hasError: false, error: null }
  }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('[ErrorBoundary] Caught error:', error, info)
  }

  reset() {
    this.setState({ hasError: false, error: null })
  }

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) return this.props.fallback

      return (
        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            padding: '3rem',
            gap: '1rem',
            color: 'var(--fg)',
          }}
        >
          <FontAwesomeIcon
            icon={faExclamationTriangle}
            style={{ fontSize: 36, color: 'var(--error)', marginBottom: '0.5rem' }}
          />
          <h2 style={{ fontSize: 18, fontWeight: 600, margin: 0 }}>Something went wrong</h2>
          {this.state.error && (
            <p
              style={{
                fontSize: 13,
                color: 'var(--fg-muted)',
                maxWidth: 480,
                textAlign: 'center',
                margin: 0,
                fontFamily: 'monospace',
              }}
            >
              {this.state.error.message}
            </p>
          )}
          <button
            onClick={() => this.reset()}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '0.5rem',
              padding: '0.5rem 1.25rem',
              background: 'var(--primary)',
              color: 'var(--primary-fg)',
              border: 'none',
              borderRadius: 'var(--radius-sm)',
              cursor: 'pointer',
              fontSize: 13,
              fontWeight: 600,
            }}
          >
            <FontAwesomeIcon icon={faRotateRight} />
            Try again
          </button>
        </div>
      )
    }

    return this.props.children
  }
}
