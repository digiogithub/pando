import { Spinner } from '@/components/ui'

/** Thin backward-compatible wrapper around the `ui` Spinner primitive. */
export default function LoadingSpinner({ size = 24 }: { size?: number }) {
  return <Spinner size={size} />
}
