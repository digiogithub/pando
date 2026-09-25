import type { ReactNode } from 'react'
import { BrandMark, type BrandMarkVariant } from './BrandMark'
import '@/styles/sections.css'

export interface SectionHeroProps {
  /** Which section identity to show (Remembrances 本, Mesnada 众). */
  variant: BrandMarkVariant
  title: string
  /** One-line description of the section; kept short, wraps to two lines max. */
  tagline?: string
  /** Right-side slot for actions or a status summary. */
  actions?: ReactNode
  className?: string
}

/**
 * A restrained identity header for a feature section (Remembrances, Mesnada,
 * the Orchestrator view): the section's brand mark, a display-type title and
 * a one-line tagline, with an optional right-side slot. Same system as the
 * rest of the brand (monoline Marfil, Álamo nodes, Bosque tile) — no new
 * colours, the identity comes from the symbol and typography alone.
 */
export function SectionHero({ variant, title, tagline, actions, className }: SectionHeroProps) {
  const classes = ['section-hero', className].filter(Boolean).join(' ')
  return (
    <header className={classes}>
      <div className="section-hero-main">
        <BrandMark variant={variant} tile size={44} className="section-hero-mark" />
        <div className="section-hero-text">
          <h2 className="section-hero-title brand-display">{title}</h2>
          {tagline && <p className="section-hero-tagline">{tagline}</p>}
        </div>
      </div>
      {actions && <div className="section-hero-actions">{actions}</div>}
    </header>
  )
}

export default SectionHero
