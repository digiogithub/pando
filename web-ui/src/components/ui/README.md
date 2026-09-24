# Pando UI primitives — how to use

Foundation for the WebUI native redesign (plan: `.kb/pando/plans/webui_native_redesign_plan.md`).
Every new or migrated component should be built from these pieces. Target look: calm, native,
Claude Desktop / Zeron: neutral surfaces, one restrained accent, hairline borders, soft radii.

## Rules

1. **No new inline `style={{}}` objects** for colour, radius, spacing or typography. Use the
   primitives, `ui.css` classes, or Tailwind utilities mapped to tokens. Inline styles are
   acceptable only for truly dynamic values (computed positions, widths from state).
2. **Tokens only.** Never hardcode hex colours; use `var(--token)` or the Tailwind utilities.
3. **Icons come from `@/components/ui/icons`** (lucide). Never import `lucide-react` directly;
   add missing icons to `icons.ts`. No new FontAwesome usages; migrate FA in the area you own
   (mapping table at the top of `icons.ts`). Default size 16 / stroke 1.75 (set by
   `<IconProvider>` in `main.tsx`); pass `size={14}` for dense rows. `File`, `Image`, `Link`
   are exported as `FileIcon`, `ImageIcon`, `LinkIcon`.
4. **One accent.** `--accent` is for the primary action, focus, selection and active state.
   Everything else is neutral. Status colours only for status, always paired with text/icon.
5. **Theme** is read/changed via `useTheme()` (`@/hooks/useTheme`); never touch
   `data-theme*` attributes or `localStorage` yourself.
6. User-visible strings go through i18n (`t(...)`); icon-only buttons need an `aria-label`.

## Tokens (`src/styles/tokens.css`)

| Group | Tokens |
|---|---|
| Surfaces | `--bg` (content), `--bg-shell` (sidebar, title bar), `--bg-raised` (user bubble, hover/selected fill), `--bg-card`, `--bg-input`, `--bg-overlay` (menus, dialogs), `--bg-hover` (softer hover), `--scrim` (modal backdrop) |
| Text | `--fg`, `--fg-muted` (secondary, >= 4.5:1), `--fg-faint` (hints/meta only, >= 3:1) |
| Lines | `--border` (hairline), `--border-strong` (inputs, emphasis) |
| Accent | `--accent`, `--accent-fg` (text on accent), `--accent-soft` (tinted fill), `--accent-hover`, `--focus-ring` |
| Status | `--danger`, `--warning`, `--success`, `--info`, each with `-soft` |
| Radii | `--radius-xs` 4, `-sm` 6, `-md` 10 (controls), `-lg` 14 (cards, popovers), `-xl` 20 (dialogs, composer), `-pill` 999 |
| Shadows | `--shadow-sm`, `--shadow-md` (popovers), `--shadow-lg` (dialogs) — tuned per mode |
| Type | `--font-sans` (Inter Variable), `--font-mono` (JetBrains Mono Variable); `--text-xs` 12, `-sm` 13, `-base` 14, `-md` 15, `-lg` 17, `-xl` 20, `-2xl` 26 |
| Motion | `--ease`, `--dur-fast` 120ms (hover/press), `--dur` 180ms (open/close) |
| Controls | `--control-h` 32px, `--control-h-sm` 28px |

The v1 alias names (`--primary`, `--sidebar-bg`, `--bg-secondary`, `--fg-dim`, `--error`,
`--space-*`...) were removed in P7 of the redesign: use the tokens above only.

### Tailwind utilities (`@theme inline` in `index.css`)

Colours: `bg-bg`, `bg-shell`, `bg-raised`, `bg-card`, `bg-input`, `bg-overlay`, `bg-hover`,
`text-fg`, `text-muted`, `text-faint`, `border-border`, `border-border-strong`, `bg-accent`,
`text-accent`, `text-accent-fg`, `bg-accent-soft`, `text-danger`, `bg-danger-soft` (same for
warning/success/info), `outline-focus`. Radii `rounded-xs|sm|md|lg|xl` and font sizes
`text-xs|sm|base|md|lg|xl|2xl` use the Pando values. `font-sans` / `font-mono` too.
The global reset lives in `@layer base`, so spacing utilities (`p-2`, `gap-3`...) work.

### Themes

`family x mode x accent`. Families: `pando` (default, neutral zinc + muted gold), `paper`
(warm parchment + terracotta), `slate` (cool blue-grey + blue), `forest` (stone + deep green).
Modes: `light | dark | system`. Accent presets: `gold, terracotta, violet, blue, green, rose,
graphite` (override only the accent pair). After editing palettes run
`bun run check:contrast` (WCAG checks for every combination + checks that `styles/themes.ts`
and the `index.html` boot script mirror `tokens.css`).

## Theme store API (`@/hooks/useTheme`)

```ts
const {
  family, mode, resolvedMode, accent,          // 'pando'.., 'light'|'dark'|'system', 'light'|'dark', AccentPreset|null
  setFamily, setMode, setAccent, toggleMode,   // toggleMode flips the resolved mode
  setTheme,                                    // 'family-mode' id (legacy ids mapped) or bare mode
  themeId, themeName, themeMode,               // v1 compat: 'pando-system', family, resolved mode
} = useTheme()
useThemeStore.getState()                       // outside React
```

## Components (`import { ... } from '@/components/ui'`)

| Component | Key props |
|---|---|
| `Button` | `variant` primary \| secondary (default) \| ghost \| danger, `size` sm \| md, `loading`, `icon`, `iconRight`, `block` |
| `IconButton` | **`aria-label` (required)**, `icon`, `variant` (default ghost), `size`, `tooltip` (node or `true` = aria-label), `active` (toggle, sets aria-pressed), `loading` |
| `Input` | native input props + `size`, `invalid` |
| `Textarea` | native props + `invalid`, `autosize`, `maxRows` |
| `Select` | native select props + `options=[{value,label,disabled}]`, `size`, `invalid`, `wrapperClassName` |
| `Switch` | `checked`, `onCheckedChange` (role="switch"; give it an `id` + label or `aria-label`) |
| `Checkbox` | `checked`, `onCheckedChange`, `indeterminate`, `label` |
| `Card` | `padding` none \| sm \| md \| lg, `elevated`, `interactive` |
| `Dialog` | `open`, `onClose`, `title`, `description`, `footer`, `size` sm \| md \| lg \| xl, `dismissible`, `hideClose`, `closeLabel` — portal, blur scrim, focus trap, Esc, focus restore. Put `data-autofocus` on the element to focus first. |
| `Popover` | `open`, `onClose`, `anchorRef`, `placement` (`bottom-start` default, flips), `offset`, `padded` — closes on outside click / Esc |
| `Menu` + `MenuItem` / `MenuSeparator` / `MenuLabel` | Menu = Popover props + `aria-label`; arrow/Home/End keys. `MenuItem`: `icon`, `hint` (shortcut), `danger`, `checked`, `onSelect` |
| `Tabs` | `items=[{value,label,icon,disabled}]`, `value`, `onChange` — underline tabs, arrow-key roving |
| `SegmentedControl` | same as Tabs + `size`; role="radiogroup" (e.g. Light/Dark/System) |
| `Badge` | `tone` neutral \| accent \| success \| warning \| danger \| info, `outline`, `dot`, `icon` |
| `Tooltip` | `content`, `placement` (default bottom), `delay` — wraps any child |
| `Kbd` | `<Kbd>⌘</Kbd><Kbd>K</Kbd>` |
| `Spinner` | `size`, `label` (omit when decorative) |
| `Divider` | `vertical` |
| `SettingsSection` | `title`, `description`, children = `SettingsRow`s (rendered as one rounded group) |
| `SettingsRow` | `label`, `description`, `htmlFor`, `stacked` (control below text), children = control |
| `EmptyState` | `icon`, `title`, `description`, `action` |

Example:

```tsx
import { Button, IconButton, SettingsSection, SettingsRow, Switch } from '@/components/ui'
import { Plus, Trash2 } from '@/components/ui/icons'

<Button variant="primary" icon={<Plus />}>{t('chat.newSession')}</Button>
<IconButton aria-label={t('common.delete')} tooltip icon={<Trash2 />} onClick={remove} />

<SettingsSection title={t('settings.general.behaviour')}>
  <SettingsRow label={t('settings.general.llmCache')} description={t('settings.general.llmCacheDescription')} htmlFor="llm-cache">
    <Switch id="llm-cache" checked={on} onCheckedChange={setOn} />
  </SettingsRow>
</SettingsSection>
```

## Class naming (`src/styles/ui.css`)

- Block: `.ui-<component>` (`.ui-btn`, `.ui-menu-item`), part: `.ui-<component>-<part>`
  (`.ui-dialog-title`), modifier: `.ui-<component>--<modifier>` (`.ui-btn--primary`).
- State via ARIA attributes, not classes: `[aria-checked]`, `[aria-selected]`,
  `[aria-invalid]`, `[aria-pressed]`, `:disabled`, `:focus-visible`.
- `ui.css` is imported as `layer(components)`: it beats Tailwind preflight and the element
  defaults in `@layer base`, while Tailwind utilities (`layer(utilities)`) passed via
  `className` win over it. Area CSS files are unlayered, so they override primitives too.
- Area-specific styles go in their own file (e.g. `styles/chat.css`) imported from `index.css`, with an area prefix (`.chat-*`,
  `.shell-*`, `.settings-*`) — never restyle `.ui-*` from another file.
- Focus: `:focus-visible { outline: 2px solid var(--focus-ring); outline-offset: 2px }`
  is global (in `@layer base`, see `index.css`); keep it (do not set `outline: none` without a replacement).
- Hover fill `--bg-raised`, transitions `var(--dur-fast) var(--ease)`.
