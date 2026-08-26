# Typography

Type is the largest surface of any layout. Get it wrong and nothing else rescues the page.

## Scale

- Pick **one** ratio and stay on it. 1.25 (major third) for dense UI, 1.333 for editorial,
  1.5 for decks read from across a room. Sizes off the scale look like mistakes.
- Cap the number of distinct sizes at five or six per artifact. A page with eleven sizes has
  no hierarchy, only noise.
- Body copy is 16-18px on the web, never below 14px. Deck body copy starts at 24px: the
  reader is metres away, not centimetres.

## Measure

- 45-75 characters per line for body text. Set `max-width: 65ch`, not a pixel width — the
  measure has to follow the font size, and `ch` is the only unit that does.
- Headings may run wider, but a heading over ~30 words is a paragraph wearing a hat.

## Rhythm

- Line height falls as size rises: ~1.6 for body, ~1.2 for display. A 48px heading at 1.6
  looks like two unrelated lines.
- Space *between* blocks belongs to the block above only in one direction. Pick
  margin-bottom or margin-top and use it everywhere, or collapsing margins will surprise you.

## Weight and contrast

- Contrast by weight and size before reaching for colour. Two weights (regular + bold) carry
  most hierarchies.
- Never fake a weight the family does not ship: synthetic bold and oblique smear the letter
  shapes.
- All-caps needs positive letter-spacing (~0.05em). Lowercase needs none.

## Font choice

- One family for text, at most one more for display. Three families is a ransom note.
- System stacks are legitimate and fast. Name a real fallback after every web font, because
  the fallback is what the first paint uses.
