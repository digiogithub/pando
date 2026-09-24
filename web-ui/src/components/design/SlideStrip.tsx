import clsx from 'clsx'
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
    <div className="design-slide-strip">
      <span className="design-slide-strip-label">{t('design.deck.slides')}</span>
      {Array.from({ length: slides }, (_, index) => index + 1).map((number) => (
        <button
          key={number}
          type="button"
          onClick={() => setSlide(number)}
          className={clsx('design-slide-btn', slide === number && 'design-slide-btn--active')}
        >
          {number}
        </button>
      ))}
    </div>
  )
}
