import { useEffect, useRef } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useDesignStore } from '@pando/client/stores/designStore'
import { hasDesktopAppBindings } from '@/services/desktopRuntime'

let lastHandledCreatedEvent: string | null = null

export default function DesignRouteEffects() {
  const connect = useDesignStore((s) => s.connect)
  const disconnect = useDesignStore((s) => s.disconnect)
  const lastCreated = useDesignStore((s) => s.lastCreated)
  const navigate = useNavigate()
  const location = useLocation()
  const lastHandledRef = useRef<string | null>(null)

  useEffect(() => {
    connect()
    return () => disconnect()
  }, [connect, disconnect])

  useEffect(() => {
    if (!lastCreated || hasDesktopAppBindings()) return
    const eventKey = `${lastCreated.artifactId}:${lastCreated.nonce}`
    if (lastHandledRef.current === eventKey || lastHandledCreatedEvent === eventKey) return
    lastHandledRef.current = eventKey
    lastHandledCreatedEvent = eventKey
    const nextPath = `/design/${lastCreated.artifactId}`
    if (location.pathname !== nextPath) navigate(nextPath)
  }, [lastCreated, location.pathname, navigate])

  return null
}
