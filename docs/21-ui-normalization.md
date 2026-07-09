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

- Color: semantic tokens in `app.css` cover text, muted text, surfaces, borders, focus, brand, success, warning, danger, and info.
- Radius: cards and controls use 8px, compact tree rows and code blocks may use 6px, pills use 999px.
- Spacing: 6/8/10/12/14/18/24px remain the active scale to match the dense desktop layout.
- Typography: Inter/Segoe/system stack, 10-15px component text, code preview uses Cascadia Code/Consolas.
- States: hover, disabled, active, error, warning, success, and info states are expressed as token-backed classes.

## Normalization rules

- New status labels should use `StatusBadge` instead of hand-coded pill styles.
- New panel titles should use `PanelHeader` so title/subtitle/action layout stays consistent.
- New right-side tabs should use `TabButton` and keep the `right-tab` class for existing layout.
- New controls should reuse `Button`/`IconButton`; only create new button CSS when behavior differs.
- Empty, loading, and error states should use `EmptyState`, `InlineEmpty`, and `ErrorMessage`, with legacy class names kept only where a panel needs local layout.
- New form rows should use `Field` so labels, spacing, and focus behavior stay consistent across settings and inspectors.
- New dropdowns should use `SelectMenu`; native `<select>` controls are intentionally avoided so hover, focus, menu shape, and compact sizing stay consistent.
