# Interaction and motion

For anything the user is meant to click, type into, or watch move: a prototype,
an app screen, a dashboard, an animated explainer.

## Design the states, not the screen

Every interactive element has more than one state, and a mockup showing only the
resting one is a mockup of a third of the design:

- **Rest, hover, focus-visible, active, disabled** for every control.
- **Empty, loading, partial, full, error** for every region that holds data.
- **Selected** where selection exists, and it must be visible without colour
  alone.

`:focus-visible` is not optional and must never be `outline: none`. It is the
only way a keyboard user knows where they are. If the default ring clashes,
replace it with a better one; do not remove it.

## Make the prototype real enough to judge

A prototype exists to answer a question. Wire the path that answers it — the
flow the user asked about, end to end, with plausible data — and leave the rest
visibly inert rather than half-working. A button that looks live and does
nothing teaches the reviewer the wrong thing.

Keep state in one place (a small JS object, not scattered DOM reads), and render
from it. Persist across reloads only when the point of the prototype is
persistence.

## Hit targets and input

- Touch targets 44×44px minimum, with real spacing between them.
- Inputs get labels, not placeholder-as-label: the placeholder disappears the
  moment it is needed.
- Never trap focus without a way out, and never open a modal without returning
  focus where it came from.

## Motion earns its place

Motion should explain a change, not decorate a page. Ask what it clarifies: what
came from where, what is loading, what just happened.

- 120–200ms for state changes on small elements, 200–320ms for something
  entering or leaving, longer only for a deliberate narrative beat.
- Ease out for things arriving, ease in for things leaving. Linear is for
  progress indicators and nothing else.
- Animate `transform` and `opacity`. Animating layout properties (width, top,
  margin) makes the browser re-lay-out every frame and looks it.
- One thing moves at a time. Three simultaneous animations read as noise.
- Nothing important may be invisible until an animation finishes.

Respect the user's setting:

```css
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after { animation-duration: .01ms !important; transition-duration: .01ms !important; }
}
```

That is a floor, not a design: for anything whose meaning depends on movement,
provide a still equivalent that carries the same information.

## Timed content

For an animation, a walkthrough or a video-shaped piece: drive it from one
timeline with a single clock, not from a chain of nested `setTimeout` calls.
Expose play, pause and a position, so the reviewer can stop on the frame they
want to talk about. Restore the position on reload — a reviewer who reloads
should not have to watch the first twenty seconds again.

## Verify by using it

Render it, then click it. `design_inspect` the control you are unsure about and
check its states. A prototype nobody has driven is a static page with extra
JavaScript.
