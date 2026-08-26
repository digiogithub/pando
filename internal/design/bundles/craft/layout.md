# Layout

## Spacing scale

- Every gap comes from the spacing scale. A one-off `padding: 13px` is the seed of drift.
- Space groups things: elements that belong together sit closer than elements that do not.
  This does more for comprehension than any border or box.

## Alignment

- Pick few alignment lines and hold them. Every new left edge is a new thing for the eye to
  track.
- Optical alignment beats mathematical alignment for icons and punctuation, but only fix
  what actually looks wrong.

## Structure

- Use CSS grid for the page skeleton and flexbox inside components. Reaching for absolute
  positioning to place page-level content means the structure is wrong.
- Let content define height. Fixed heights turn a translated string into an overflow bug.
- One column of content beats two whenever the two are not genuinely parallel.

## Responsiveness

- Design the narrow layout first: it forces priority decisions the wide layout lets you
  dodge.
- Break at content, not at device names. Add a breakpoint where the layout starts looking
  wrong, not at 768px because a framework said so.
- Nothing may scroll the page horizontally. Wide tables, diagrams and code blocks scroll
  inside their own container.

## Density

- Whitespace is not wasted space; it is what makes the used space legible.
- Deck slides carry one idea each. If a slide needs a scrollbar, it is two slides.
