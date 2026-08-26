# Colour

## Roles before values

Design with roles — background, surface, text, muted, accent, border — and let the design
system supply the values. A hardcoded hex is a decision nobody can revise later.

## Contrast is a requirement, not a preference

- Body text: contrast ratio >= 4.5:1 against its own background. Large text (>= 24px, or
  19px bold): >= 3:1.
- Interactive borders and focus rings: >= 3:1 against adjacent colour.
- Check the ratio against the colour actually behind the text, not the page background. Text
  on a card sits on the card.

## Restraint

- One accent. A second "accent" is just a colour with no job.
- Saturated colour reads as *importance*. Colouring everything says nothing is important.
- Large areas take low-saturation colour; small areas can take full saturation. A fully
  saturated hero background makes every foreground element unreadable.

## Never colour alone

Colour must never be the only carrier of meaning: pair it with an icon, a label, or a shape.
Around 1 in 12 men cannot separate your red state from your green one.

## Dark mode

- Do not invert. Dark surfaces need *less* saturated accents and lighter, thinner borders.
- Pure black (#000) with pure white (#fff) text vibrates. Use a near-black surface and a
  near-white text colour.
