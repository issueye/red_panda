# UI Style Normalization Implementation Plan

> **For Codex:** Implement the tasks in order and verify each boundary before continuing.

**Goal:** Use one global UI specification, increase conversation-body contrast, and split the largest frontend units by feature without changing behavior.

**Architecture:** Keep `src/styles/app.css` as the ordered stylesheet manifest and move complete top-level CSS rules into feature-owned files. Keep `App.jsx` as the application state controller while extracting layout-only behavior into reusable app-shell components.

**Tech Stack:** React 18, Vite 8, PostCSS, Playwright, Node test runner.

---

### Task 1: Split the stylesheet entry point

**Files:**
- Modify: `modules/desktop/frontend/src/styles/app.css`
- Modify: `modules/desktop/frontend/src/styles/tokens.css`
- Modify: `modules/desktop/frontend/src/styles/base.css`
- Modify: `modules/desktop/frontend/src/styles/layout.css`
- Modify: `modules/desktop/frontend/src/styles/responsive.css`
- Modify: `modules/desktop/frontend/src/styles/components/*.css`
- Create: `modules/desktop/frontend/src/styles/components/buttons.css`

1. Parse the current monolithic stylesheet with PostCSS.
2. Move complete top-level rules into functional files while preserving their original cascade order.
3. Replace `app.css` with ordered `@import` statements.
4. Run `npm run build`; expect a successful Vite production build.

### Task 2: Normalize global tokens and chat contrast

**Files:**
- Modify: `modules/desktop/frontend/src/styles/tokens.css`
- Modify: `modules/desktop/frontend/src/styles/components/chat.css`
- Modify: `docs/21-ui-normalization.md`

1. Add semantic global tokens for conversation text and shared overlays.
2. Replace chat-content literal/fallback colors with the semantic tokens.
3. Apply the stronger text token to plain and Markdown message bodies while keeping metadata visually secondary.
4. Document the global token and component ownership rules.

### Task 3: Split application layout components

**Files:**
- Create: `modules/desktop/frontend/src/components/app/PanelResizer.jsx`
- Create: `modules/desktop/frontend/src/components/app/RightInspector.jsx`
- Modify: `modules/desktop/frontend/src/App.jsx`

1. Extract keyboard and pointer behavior for panel resizing into `PanelResizer`.
2. Extract right-panel rail, drawer, tabs, backdrop, and content into `RightInspector`.
3. Keep application state, gateway events, and session actions in `App.jsx`.
4. Run unit tests and the production build; expect no behavior changes.

### Task 4: Visual and regression verification

**Files:**
- Test: `modules/desktop/frontend/tests/workflow-panels.spec.js`
- Test: `modules/desktop/frontend/tests/responsive-layout.spec.js`
- Test: `modules/desktop/frontend/tests/accessibility-interactions.spec.js`

1. Run `npm test`; expect all Node tests to pass.
2. Run the focused Playwright suites for workflow, responsive layout, and accessibility.
3. Capture desktop and compact screenshots and inspect message contrast, spacing, focus states, and overlap.
4. Run the full Playwright suite when the focused suites pass.

