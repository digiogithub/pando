import type { SVGProps } from 'react'
import '@/styles/brand.css'

/**
 * Pando v1 brand marks (source of truth: assets/pando-brand-v1/).
 *
 * - `pando`        木 tree: one root, many trunks.
 * - `remembrances` 本 root/book: the gold stroke at the root is stored memory.
 * - `mesnada`      众 crowd: the lord (solid node) and the retinue (ring nodes).
 *
 * The bare mark is themeable: strokes use `currentColor` (defaults to --fg via
 * the parent's text colour) and circuit nodes use `--brand-node` (Álamo on
 * dark, Álamo oscuro on light). `tile` draws the app-icon form instead: fixed
 * Bosque rounded square, Marfil strokes, Álamo nodes, identical in every theme.
 */
export type BrandMarkVariant = 'pando' | 'remembrances' | 'mesnada'

export interface BrandMarkProps extends Omit<SVGProps<SVGSVGElement>, 'children'> {
  variant?: BrandMarkVariant
  /** Rendered width/height in px. */
  size?: number
  /** App-icon form (Bosque tile). */
  tile?: boolean
  /**
   * Heavier strokes + filled nodes for small sizes, as in the `*-icon-small`
   * assets. Defaults to true at 24px and below.
   */
  bold?: boolean
  /** Animate the circuit nodes (busy indicator). Honours reduced motion. */
  pulse?: boolean
  /** Accessible label; when omitted the mark is decorative (aria-hidden). */
  title?: string
}

interface Geometry {
  strokes: string[]
  /** Node stroke that is part of the ink in the gold colour (Remembrances root). */
  goldStrokes?: string[]
  /** Ring nodes (outlined at regular weight, filled when bold). */
  nodes: Array<[number, number]>
  /** Always-solid nodes (Mesnada lord). */
  solidNodes?: Array<[number, number]>
  /** Vertical centre used to optically centre the mark inside a tile. */
  cy: number
}

const GEOMETRY: Record<BrandMarkVariant, Geometry> = {
  pando: {
    strokes: ['M20 36H80', 'M50 12V80', 'M50 38C45 54 34 66 21 73', 'M50 38C55 54 66 66 79 73'],
    nodes: [[16, 76], [84, 76], [50, 87]],
    cy: 50.5,
  },
  remembrances: {
    strokes: ['M20 36H80', 'M50 12V88', 'M50 38C45 54 34 66 21 73', 'M50 38C55 54 66 66 79 73'],
    goldStrokes: ['M39 72H61'],
    nodes: [[16, 76], [84, 76]],
    cy: 49.9,
  },
  mesnada: {
    strokes: [
      'M50 23C45 36 35 45 21 51', 'M50 23C55 36 65 45 79 51',
      'M29 66C26 75 20 82 12 87', 'M29 66C32 75 38 82 46 87',
      'M71 66C68 75 62 82 54 87', 'M71 66C74 75 80 82 88 87',
    ],
    nodes: [[29, 58], [71, 58]],
    solidNodes: [[50, 14]],
    cy: 49.05,
  },
}

export function BrandMark({
  variant = 'pando',
  size = 20,
  tile = false,
  bold,
  pulse = false,
  title,
  className,
  ...rest
}: BrandMarkProps) {
  const g = GEOMETRY[variant]
  const heavy = bold ?? size <= 24
  const strokeW = heavy ? 9 : 6.6
  const nodeR = heavy ? 6.6 : 5
  const ink = tile ? 'var(--brand-marfil)' : 'currentColor'
  const gold = tile ? 'var(--brand-alamo)' : 'var(--brand-node)'
  const scale = heavy ? 0.76 : 0.72

  const body = (
    <>
      <g fill="none" stroke={ink} strokeWidth={strokeW} strokeLinecap="round" strokeLinejoin="round">
        {g.strokes.map((d) => <path key={d} d={d} />)}
      </g>
      {g.goldStrokes?.map((d) => (
        <path key={d} d={d} fill="none" stroke={gold} strokeWidth={strokeW} strokeLinecap="round" />
      ))}
      {g.solidNodes?.map(([cx, cy]) => (
        <circle key={`s${cx}-${cy}`} className="brand-mark-node" cx={cx} cy={cy} r={heavy ? 7.2 : 5.6} fill={gold} />
      ))}
      {g.nodes.map(([cx, cy]) =>
        heavy ? (
          <circle key={`${cx}-${cy}`} className="brand-mark-node" cx={cx} cy={cy} r={nodeR} fill={gold} />
        ) : (
          <circle key={`${cx}-${cy}`} className="brand-mark-node" cx={cx} cy={cy} r={nodeR} fill="none" stroke={gold} strokeWidth={5.6} />
        ),
      )}
    </>
  )

  const classes = ['brand-mark', pulse && 'brand-mark--pulse', className].filter(Boolean).join(' ')
  const a11y = title ? { role: 'img', 'aria-label': title } : { 'aria-hidden': true as const }

  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox={tile ? '0 0 100 100' : '5 5 90 90'}
      width={size}
      height={size}
      className={classes}
      focusable="false"
      {...a11y}
      {...rest}
    >
      {tile ? (
        <>
          <rect width="100" height="100" rx="22.5" fill="var(--brand-bosque)" />
          <g transform={`translate(50 50) scale(${scale}) translate(-50 -${g.cy})`}>{body}</g>
        </>
      ) : (
        body
      )}
    </svg>
  )
}

export default BrandMark
