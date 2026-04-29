import { useCallback, useEffect, useRef } from 'react'

/**
 * Manages browser desktop notifications.
 *
 * On mount, requests notification permission if it hasn't been decided yet.
 * Exposes `notify` to fire a notification with a title/body and an optional
 * click handler that focuses the current tab and navigates to a URL.
 */
export function useDesktopNotifications() {
  const permissionRef = useRef<NotificationPermission>(
    typeof Notification !== 'undefined' ? Notification.permission : 'denied',
  )

  useEffect(() => {
    if (typeof Notification === 'undefined') return
    if (Notification.permission === 'default') {
      Notification.requestPermission().then((perm) => {
        permissionRef.current = perm
      })
    }
  }, [])

  const notify = useCallback(
    (title: string, options?: { body?: string; icon?: string; onClick?: () => void }) => {
      if (typeof Notification === 'undefined') return
      if (Notification.permission !== 'granted') return

      const notification = new Notification(title, {
        body: options?.body,
        icon: options?.icon ?? '/pando-icon.svg',
      })

      notification.onclick = () => {
        window.focus()
        options?.onClick?.()
        notification.close()
      }
    },
    [],
  )

  return { notify }
}
