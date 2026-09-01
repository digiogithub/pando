# Print and fixed-size canvases

Anything that ends as a PDF, a printed page, or an image at a fixed size obeys
different rules from a page that reflows. Decide which one you are building
before you write the markup: converting afterwards means rebuilding.

## Pick the canvas first

- **Flowing document** (report, memo, résumé, letter): pages of a fixed paper
  size, content flowing across them. Set the page in CSS, let the content break.
- **Fixed canvas** (poster, flier, social post, banner, infographic, ad): one
  element with an explicit pixel `width` — and `height` when the size is fixed —
  and everything laid out inside it. No breakpoints, no reflow.
- **Slide**: a fixed canvas repeated, one per page on export.

If the brief does not say the size or the medium, ask before choosing. "A4 or
US Letter" and "print or screen" change the whole layout.

## Page setup

```css
@page { size: A4; margin: 18mm 16mm; }
```

- Use physical units (`mm`, `pt`, `in`) for anything that must measure correctly
  on paper. `px` is a screen unit and will not survive a printer honestly.
- Body text 10–12pt minimum for a document; captions no smaller than 8pt.
- Keep the margin wide enough to survive a printer's unprintable edge: 12mm is
  the floor, 15–20mm reads better.
- Full-bleed artwork needs 3mm of overhang past the trim on every bleeding edge,
  and nothing meaningful within 5mm of the trim.

## Breaks

Control them explicitly; the defaults will split a heading from its section.

```css
h2, h3 { break-after: avoid; }
figure, table, li { break-inside: avoid; }
.page-break { break-before: page; }
```

Orphans and widows: `orphans: 3; widows: 3;` on body copy. A single line
stranded at the top of a page is the most visible printing mistake there is.

## What does not survive printing

- Background colours and images, unless the exporter is told to keep them.
  Pando's PDF export prints backgrounds; a browser's Ctrl-P dialog may not.
  Never rely on a background to carry meaning that the text does not.
- Hover, focus and transition states. Anything only reachable by interaction is
  invisible on paper — print the state that matters, or print both.
- Scrollable regions. A table that scrolls on screen is a truncated table on
  paper: let it break across pages, or set it smaller.
- Links. Nobody clicks paper. Where the URL matters, print it.
- Dark themes. A dark page is a page of wet ink. Print on a light ground unless
  the user asked otherwise.

## Colour on paper

Screen colour is additive and paper colour is not: saturated screen blues,
greens and oranges print duller and darker. Keep large areas light, reserve
saturation for small marks, and never rely on a hue difference alone to separate
two things — pair it with a shape, a label or a weight difference.

Pure black body text prints heavy on coated stock; a near-black reads cleaner.
Hairlines below 0.25pt may vanish altogether.

## Verify

Export it (`design_export` with format `pdf`) and count the pages. A deck must
produce exactly one page per slide; a document must break where you told it to.
A page count that does not match the design means the print styles were edited
into something else, and the fix is in the CSS, not in the export.
