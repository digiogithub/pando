import type { AnchorHTMLAttributes, MouseEvent } from 'react'
import { openExternal } from '@/services/desktopRuntime'

function isAbsoluteHttpUrl(href: string): boolean {
  return /^https?:\/\//i.test(href)
}

export default function MarkdownLink({ href, onClick, ...props }: AnchorHTMLAttributes<HTMLAnchorElement>) {
  const handleClick = (event: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(event)
    if (event.defaultPrevented || !href) return
    if (href.startsWith('design://')) {
      event.preventDefault()
      return
    }
    if (!isAbsoluteHttpUrl(href)) return
    event.preventDefault()
    void openExternal(href)
  }

  return <a {...props} href={href} onClick={handleClick} />
}
