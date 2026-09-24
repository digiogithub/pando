import { Component } from 'react'
import type { ReactNode, ErrorInfo } from 'react'
import { TriangleAlert, RotateCw } from '@/components/ui/icons'
import { Button } from '@/components/ui'

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
        <div className="flex flex-col items-center justify-center gap-4 p-12 text-center text-fg">
          <TriangleAlert size={36} className="text-danger" />
          <h2 className="text-lg font-semibold">Something went wrong</h2>
          {this.state.error && (
            <p className="max-w-md font-mono text-sm text-muted">{this.state.error.message}</p>
          )}
          <Button variant="primary" icon={<RotateCw size={14} />} onClick={() => this.reset()}>
            Try again
          </Button>
        </div>
      )
    }

    return this.props.children
  }
}
