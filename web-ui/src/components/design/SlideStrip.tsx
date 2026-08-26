import { useTranslation } from 'react-i18next'
import { useDesignStore } from '@pando/client/stores/designStore'

interface SlideStripProps {
  slides: number
}

/**
 * SlideStrip is deck mode's navigation: one button per slide, driving the
 * preview bridge through the store.
 *
 * It shows numbers rather than thumbnails on purpose — a thumbnail per slide
 * would mean one screenshot round trip per slide on every render, which is the
 * most expensive thing the Studio could do to a deck under active iteration.
 */
export default function SlideStrip({ slides }: SlideStripProps) {
  const { t } = useTranslation()
  const slide = useDesignStore((s) => s.slide)
  const setSlide = useDesignStore((s) => s.setSlide)

  if (slides <= 0) return null

  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '0.3rem',
        padding: '0.35rem 0.5rem',
        borderTop: '1px solid var(--border)',
        overflowX: 'auto',
        flexShrink: 0,
      }}
    >
      <span style={{ fontSize: 10, color: 'var(--fg-muted)', flexShrink: 0, marginRight: '0.25rem' }}>
        {t('design.deck.slides')}
      </span>
      {Array.from({ length: slides }, (_, index) => index + 1).map((number) => (
        <button
          key={number}
          onClick={() => setSlide(number)}
          style={{
            flexShrink: 0,
            minWidth: 28,
            padding: '0.2rem 0.4rem',
            fontSize: 11,
            background: slide === number ? 'var(--primary)' : 'var(--surface)',
            color: slide === number ? 'white' : 'var(--fg-muted)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            cursor: 'pointer',
          }}
        >
          {number}
        </button>
      ))}
    </div>
  )
}
