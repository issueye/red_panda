# UI normalization

## Current UI style

The desktop frontend is a dense operational workspace: top bar, left session rail, central conversation, right inspector tabs, settings drawer, and bottom status bar. The visual language is utilitarian and compact, with light surfaces, quiet borders, small type, and red-panda orange as the primary action color.

## Component inventory

- Shell: `TopBar`, `Sidebar`, `ChatPanel`, right inspector tabs, `StatusBar`.
- Conversation: message rows, permission cards, tool-call cards, composer.
- Inspectors: workspace tree/preview, subagent list, run activity timeline, memory editor/list.
- Settings: modal drawer, form rows, profile panel, footer actions.
- Shared UI now normalized through `Button`, `IconButton`, `Badge`, `StatusBadge`, `PanelHeader`, `TabButton`, `SelectMenu`, `Field`, `EmptyState`, `InlineEmpty`, and `ErrorMessage`.

## Design tokens

- Color: semantic tokens in `src/styles/tokens.css` cover text hierarchy, conversation text, surfaces, borders, overlays, focus, brand, success, warning, danger, and info.
- Radius: controls use 6px, framed content uses 8px, compact rows use 4px, and status badges use the pill token.
- Spacing: all shared components use the 4px-based `--space-*` scale; feature CSS may use an intermediate value only when dense alignment requires it.
- Typography: the global UI and mono stacks plus `--text-*`, `--leading-*`, and `--font-*` scales are the source of truth.
- Motion and elevation: shared durations, easing, and shadows live in `tokens.css`; feature styles must not introduce a competing focus ring or accent color.
- States: hover, disabled, active, error, warning, success, and info states are expressed as token-backed classes.

## Normalization rules

- New status labels should use `StatusBadge` instead of hand-coded pill styles.
- New panel titles should use `PanelHeader` so title/subtitle/action layout stays consistent.
- New right-side tabs should use `TabButton` and keep the `right-tab` class for existing layout.
- New controls should reuse `Button`/`IconButton`; only create new button CSS when behavior differs.
- Empty, loading, and error states should use `EmptyState`, `InlineEmpty`, and `ErrorMessage`, with legacy class names kept only where a panel needs local layout.
- New form rows should use `Field` so labels, spacing, and focus behavior stay consistent across settings and inspectors.
- New dropdowns should use `SelectMenu`; native `<select>` controls are intentionally avoided so hover, focus, menu shape, and compact sizing stay consistent.
- `app.css` is an import manifest only. New feature styles belong in the closest file under `styles/components`, while cross-feature primitives belong in `tokens.css`, `base.css`, `layout.css`, or `components/ui.css`.
- Conversation body copy uses `--color-conversation-text`; metadata, timestamps, placeholders, and collapsed reasoning remain on muted text tokens.
