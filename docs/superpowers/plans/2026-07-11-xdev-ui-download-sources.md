# XDev UI Redesign and Download Source Governance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redesign every XDev GUI page as a minimal modern desktop application using neutral grays and an indigo accent, and verify, repair, and document all built-in download sources.

**Architecture:** Establish tests, one semantic token system, a shared Wails application shell, and focused Vue page components before migrating the existing `AppContent.vue` page-by-page. Build download auditing as an independent Go workflow that parses manifests, probes concrete official and mirror archive URLs, fully verifies Windows x64 artifacts, lightly probes Linux/macOS artifacts, and produces reproducible JSON and Markdown reports.

**Tech Stack:** Wails v2, Vue 3, Naive UI, Vite 5, Go, YAML manifests, Vitest, Vue Test Utils, Playwright, and axe-core.

## Global Constraints

- Continue supporting both dark and light themes.
- Use neutral gray surfaces, graphite dark backgrounds, soft gray-white light backgrounds, and indigo accents.
- Do not use neon cyan, broad decorative gradients, glowing borders, or decorative glassmorphism.
- Preserve the frameless Wails window and all `--wails-draggable` behavior.
- Preserve the existing `xdev-theme` local-storage contract.
- Redesign every existing page without removing product behavior.
- Support desktop windows from `1024x640` through `1920x1080`.
- Support Windows display scaling at 125%, 150%, and 200%.
- Maintain official sources and trusted mirrors as peer candidates with automatic fallback.
- Fully verify Windows x64 content and SHA-256; probe Linux/macOS reachability, redirects, and archive existence.
- Never treat a reachable mirror root as proof that a concrete archive is downloadable.
- Whenever an archive SHA, version, or platform mapping changes, regenerate the corresponding manifest signature.
- Do not modify, revert, or commit unrelated worktree changes.

---

## Workstream Boundaries

The implementation has two independently testable workstreams followed by integrated acceptance:

1. **UI redesign:** tests, token system, application shell, shared components, page migrations, accessibility, and visual regression.
2. **Download governance:** deterministic audit tooling, live source audits, mirror selection fixes, special installer fixes, manifest updates, signatures, and CI.
3. **Integrated acceptance:** complete GUI, download, accessibility, and production build verification.

The UI work must not be blocked by live network availability. Live audit failures must distinguish deterministic source failures from transient network failures.

## Target File Structure

### Frontend

- `frontend/src/theme/naive-theme.js`: maps semantic XDev tokens to Naive UI overrides.
- `frontend/src/navigation/navigation.js`: one navigation and page-capability registry shared by sidebar and command palette.
- `frontend/src/layouts/AppShell.vue`: composes title bar, sidebar, toolbar, and active page.
- `frontend/src/layouts/AppTitleBar.vue`: frameless title bar and Wails window controls.
- `frontend/src/layouts/AppSidebar.vue`: grouped navigation with expanded and compact modes.
- `frontend/src/layouts/PageToolbar.vue`: page title, search, filters, and primary actions.
- `frontend/src/pages/*Page.vue`: one focused component per product page.
- `frontend/src/components/`: reusable visual and interaction patterns.
- `frontend/src/composables/useTools.js`: tool catalog loading, search, and filtering.
- `frontend/src/composables/useInstall.js`: installation lifecycle and Wails event handling.
- `frontend/test/`: smoke and unit tests with Wails mocks.
- `frontend/e2e/`: Playwright behavior, visual, responsive, and accessibility tests.

### Download Governance

- `cmd/linkaudit/main.go`: CLI entry point for reproducible manifest audits.
- `internal/linkaudit/types.go`: audit request, result, and report models.
- `internal/linkaudit/audit.go`: URL probing, redirect checks, range requests, downloads, magic-byte detection, and hash verification.
- `internal/linkaudit/report.go`: deterministic JSON and Markdown report output.
- `internal/linkaudit/audit_test.go`: local HTTP fixtures for all deterministic network behaviors.
- `docs/download-source-policy.md`: source precedence, verification rules, and release requirements.
- `build/download-audit-*.json`: generated machine-readable reports, not source-of-truth code.
- `build/download-audit-*.md`: generated human-readable reports, not source-of-truth code.

---

## Task 1: Establish the Frontend Test and Visual Baseline

**Files:**
- Modify: `frontend/package.json`
- Modify: `frontend/vite.config.js`
- Modify: `frontend/test/smoke.mjs`
- Create: `frontend/vitest.config.js`
- Create: `frontend/test/setup.js`
- Create: `frontend/test/mocks/wails.js`
- Create: `frontend/test/App.spec.js`
- Create: `frontend/playwright.config.js`
- Create: `frontend/e2e/app-shell.spec.js`
- Create: `frontend/e2e/accessibility.spec.js`

**Interfaces:**
- Produces: `installWailsMocks(overrides?: object): void` for all unit and browser tests.
- Produces: npm scripts `test:unit`, `test:e2e`, and `test:e2e:update`.
- Consumes: existing `window.go.gui.App` calls and Wails runtime behavior.

- [ ] **Step 1: Add test dependencies and scripts**

Add Vitest, Vue Test Utils, jsdom, Playwright, and axe-core. Keep `npm test` as the fast smoke test and add explicit unit/E2E scripts.

```json
{
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "test": "node test/smoke.mjs",
    "test:unit": "vitest run",
    "test:e2e": "playwright test",
    "test:e2e:update": "playwright test --update-snapshots",
    "preview": "vite preview"
  }
}
```

- [ ] **Step 2: Add a reusable Wails mock**

Define no-op window controls, deterministic tool/config responses, and event listener cleanup in `frontend/test/mocks/wails.js`. Tests must override only the methods relevant to each behavior.

- [ ] **Step 3: Write a failing application mount test**

Assert that `App.vue` mounts with mocked `IsInitialized`, renders the application shell when initialized, and renders Setup Wizard when uninitialized.

- [ ] **Step 4: Run the test and confirm the baseline failure**

Run: `npm run test:unit -- App.spec.js`

Expected: FAIL until jsdom and Wails mocks are correctly configured.

- [ ] **Step 5: Configure Vitest and make the mount test pass**

Use the Vue plugin, jsdom, and `frontend/test/setup.js`. Do not invoke real filesystem or environment mutations.

- [ ] **Step 6: Add Playwright baseline coverage**

Capture Dashboard, tool catalog, Settings, Setup Wizard, tool detail drawer, and install progress at `1024x640` and `1440x900`, in dark/light and Chinese/English combinations.

- [ ] **Step 7: Verify the baseline**

Run:

```powershell
npm test
npm run test:unit
npm run build
npm run test:e2e
```

Expected: all commands exit `0`; screenshots are generated for deliberate review.

- [ ] **Step 8: Commit the test baseline**

```powershell
git add frontend/package.json frontend/vite.config.js frontend/test frontend/e2e frontend/vitest.config.js frontend/playwright.config.js
git commit -m "test: establish frontend behavior baseline"
```

Skip the commit when the workspace is not a Git repository.

---

## Task 2: Create One Semantic Design Token System

**Files:**
- Modify: `frontend/src/styles/theme.css`
- Modify: `frontend/src/App.vue`
- Modify: `frontend/index.html`
- Modify: `frontend/test/smoke.mjs`
- Create: `frontend/src/theme/naive-theme.js`
- Create: `frontend/test/theme.spec.js`

**Interfaces:**
- Produces: `getNaiveThemeOverrides(mode: 'dark' | 'light'): GlobalThemeOverrides`.
- Produces: CSS variables prefixed with `--xdev-` for all product styles.
- Consumes: existing `data-theme` and `xdev-theme` behavior.

- [ ] **Step 1: Write failing token consistency tests**

Assert that both themes define all semantic surface, text, border, accent, status, focus, radius, spacing, shadow, and motion variables. Assert that these legacy values are absent from application styles:

```text
#00e5ff
#00f0ff
rgba(0, 229, 255
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `npm run test:unit -- theme.spec.js`

Expected: FAIL because the semantic set is incomplete and old cyan values remain.

- [ ] **Step 3: Replace the palette with the approved light tokens**

```css
[data-theme="light"] {
  --xdev-bg-app: #f6f7f9;
  --xdev-bg-panel: #ffffff;
  --xdev-bg-subtle: #f0f2f5;
  --xdev-bg-elevated: #ffffff;
  --xdev-accent: #4f46e5;
  --xdev-accent-hover: #4338ca;
  --xdev-accent-soft: #eef2ff;
  --xdev-text-primary: #18181b;
  --xdev-text-secondary: #52525b;
  --xdev-text-muted: #71717a;
  --xdev-border-subtle: #e4e4e7;
  --xdev-border-strong: #d4d4d8;
}
```

- [ ] **Step 4: Replace the palette with the approved dark tokens**

```css
:root,
[data-theme="dark"] {
  --xdev-bg-app: #18181b;
  --xdev-bg-panel: #202023;
  --xdev-bg-subtle: #27272a;
  --xdev-bg-elevated: #2d2d31;
  --xdev-accent: #818cf8;
  --xdev-accent-hover: #a5b4fc;
  --xdev-accent-soft: rgba(99, 102, 241, 0.16);
  --xdev-text-primary: #fafafa;
  --xdev-text-secondary: #d4d4d8;
  --xdev-text-muted: #a1a1aa;
  --xdev-border-subtle: #34343a;
  --xdev-border-strong: #484850;
}
```

- [ ] **Step 5: Add status and interaction tokens**

Use independent semantic status colors and a visible focus ring. Add a reduced-motion branch that disables non-essential animation.

```css
--xdev-success: #16a34a;
--xdev-warning: #d97706;
--xdev-error: #dc2626;
--xdev-info: #2563eb;
--xdev-focus-ring: 0 0 0 3px rgba(99, 102, 241, 0.35);
```

- [ ] **Step 6: Move Naive UI overrides to `naive-theme.js`**

Remove the duplicated dark/light override blocks from `App.vue`. Keep theme selection in `App.vue`, but make the override map a named imported function.

- [ ] **Step 7: Synchronize the pre-mount shell**

Update `frontend/index.html` so the first frame uses the approved graphite background and neutral text without a cyan flash.

- [ ] **Step 8: Verify tokens, build, and both visual themes**

Run:

```powershell
npm test
npm run test:unit -- theme.spec.js
npm run build
npm run test:e2e
```

Expected: no undefined CSS variables and no legacy neon values.

- [ ] **Step 9: Commit the token system**

```powershell
git add frontend/src/styles/theme.css frontend/src/theme/naive-theme.js frontend/src/App.vue frontend/index.html frontend/test
git commit -m "feat: establish indigo desktop theme"
```

---

## Task 3: Extract the Application Shell and Navigation Registry

**Files:**
- Modify: `frontend/src/AppContent.vue`
- Modify: `frontend/src/App.vue`
- Create: `frontend/src/layouts/AppShell.vue`
- Create: `frontend/src/layouts/AppTitleBar.vue`
- Create: `frontend/src/layouts/AppSidebar.vue`
- Create: `frontend/src/layouts/PageToolbar.vue`
- Create: `frontend/src/navigation/navigation.js`
- Create: `frontend/src/components/XDevLogo.vue`
- Create: `frontend/test/AppShell.spec.js`
- Create: `frontend/test/navigation.spec.js`

**Interfaces:**
- Produces: `navigationGroups: NavigationGroup[]`.
- Produces: `findPageDefinition(key: string): PageDefinition | undefined`.
- Produces: `AppShell` events `select-view` and `open-command-palette`.
- Consumes: Wails window controls and existing selected-view state.

```js
/** @typedef {{ key: string, labelKey: string, icon: object, searchable: boolean }} PageDefinition */
/** @typedef {{ key: string, labelKey: string, items: PageDefinition[] }} NavigationGroup */
```

- [ ] **Step 1: Write failing navigation registry tests**

Assert that every current page key appears exactly once and that Command Palette entries derive from the same registry.

- [ ] **Step 2: Implement the registry and remove exclusion chains**

Replace the long `selectedView !== ...` search condition with `currentPage.searchable`.

- [ ] **Step 3: Write failing shell interaction tests**

Cover minimize, maximize/restore, close, title-bar double-click, sidebar selection, compact mode, and Ctrl/Cmd+K.

- [ ] **Step 4: Extract `AppTitleBar.vue` and `XDevLogo.vue`**

Use `currentColor` and semantic tokens. Every window control must have an accessible name and remain in a no-drag region.

- [ ] **Step 5: Extract sidebar and toolbar**

Support expanded and compact modes. At narrow widths, compact the sidebar and wrap toolbar controls without horizontal page overflow.

- [ ] **Step 6: Compose `AppShell.vue`**

Keep active page state in the current parent for this task. Do not introduce Vue Router.

- [ ] **Step 7: Verify shell behavior**

Run:

```powershell
npm run test:unit -- navigation.spec.js AppShell.spec.js
npm run build
npm run test:e2e -- app-shell.spec.js
```

- [ ] **Step 8: Commit the application shell**

```powershell
git add frontend/src/layouts frontend/src/navigation frontend/src/components/XDevLogo.vue frontend/src/AppContent.vue frontend/test
git commit -m "refactor: extract application shell"
```

---

## Task 4: Build Shared Page and Interaction Components

**Files:**
- Create: `frontend/src/components/PageHeader.vue`
- Create: `frontend/src/components/SectionCard.vue`
- Create: `frontend/src/components/StatusBadge.vue`
- Create: `frontend/src/components/EmptyState.vue`
- Create: `frontend/src/components/AsyncState.vue`
- Create: `frontend/src/components/FormSection.vue`
- Create: `frontend/src/components/CopyableValue.vue`
- Create: `frontend/src/components/ResponsiveDataTable.vue`
- Create: `frontend/src/components/ConfirmAction.vue`
- Create: `frontend/test/components/shared-components.spec.js`

**Interfaces:**
- Produces: common loading, empty, failure, table, copy, form, status, and confirmation contracts.
- Consumes: semantic tokens and Naive UI primitives.

- [ ] **Step 1: Write failing tests for shared states**

Test loading, empty, error, retry, keyboard activation, copy success/failure, destructive confirmation, and icon-button accessible names.

- [ ] **Step 2: Implement page headings and section cards**

Provide named slots for title, description, primary action, secondary actions, and body. Avoid page-specific business logic.

- [ ] **Step 3: Implement `AsyncState` and `EmptyState`**

Errors must have a visible message and optional retry action. Status must never rely only on color.

- [ ] **Step 4: Implement `CopyableValue`**

Use the monospace token, middle truncation for paths/hashes where practical, a tooltip with the full value, and keyboard-accessible copy.

- [ ] **Step 5: Implement responsive data display**

At narrow widths, hide columns marked secondary and permit a card renderer. Always retain identity, status, and actions.

- [ ] **Step 6: Implement confirmation behavior**

Return focus to the trigger after close and require explicit confirmation for destructive operations.

- [ ] **Step 7: Run component tests and axe checks**

Run:

```powershell
npm run test:unit -- shared-components.spec.js
npm run test:e2e -- accessibility.spec.js
```

- [ ] **Step 8: Commit shared components**

```powershell
git add frontend/src/components frontend/test/components
git commit -m "feat: add shared desktop interface components"
```

---

## Task 5: Migrate Dashboard and Tool Catalog

**Files:**
- Modify: `frontend/src/AppContent.vue`
- Create: `frontend/src/pages/DashboardPage.vue`
- Create: `frontend/src/pages/ToolCatalogPage.vue`
- Create: `frontend/src/components/MetricCard.vue`
- Create: `frontend/src/components/ToolTable.vue`
- Create: `frontend/src/components/ToolCard.vue`
- Create: `frontend/src/composables/useTools.js`
- Create: `frontend/test/pages/DashboardPage.spec.js`
- Create: `frontend/test/pages/ToolCatalogPage.spec.js`

**Interfaces:**
- Produces: `useTools()` with `tools`, `loading`, `error`, `searchQuery`, `hideUnavailable`, `filteredTools`, `loadTools`, and `retry`.
- Consumes: existing Wails tool and dashboard APIs.

- [ ] **Step 1: Write failing composable and page tests**

Cover loading, API failure, retry, search, categories, unavailable filtering, dynamic data, empty results, and installation entry points.

- [ ] **Step 2: Implement `useTools.js`**

Move catalog loading and filtering without changing backend method names or result shapes.

- [ ] **Step 3: Build the Dashboard**

Use neutral metric surfaces, prominent numbers, restrained status accents, copyable paths, quick actions, and installed-tool summary.

- [ ] **Step 4: Build the catalog list and card fallback**

Desktop uses a dense list; narrow windows use cards. Preserve search, category, availability, detail, and install behavior.

- [ ] **Step 5: Remove migrated Dashboard/catalog markup from `AppContent.vue`**

Do not leave hidden duplicate components or page-specific CSS.

- [ ] **Step 6: Verify pages**

Run:

```powershell
npm run test:unit -- DashboardPage.spec.js ToolCatalogPage.spec.js
npm run build
npm run test:e2e
```

- [ ] **Step 7: Commit the migration**

```powershell
git add frontend/src/pages frontend/src/components frontend/src/composables/useTools.js frontend/src/AppContent.vue frontend/test/pages
git commit -m "feat: redesign dashboard and tool catalog"
```

---

## Task 6: Migrate Favorites, Tool Details, and Installation Progress

**Files:**
- Modify: `frontend/src/AppContent.vue`
- Create: `frontend/src/pages/FavoritesPage.vue`
- Create: `frontend/src/components/ToolDetailDrawer.vue`
- Create: `frontend/src/components/VersionList.vue`
- Create: `frontend/src/components/InstallProgressDialog.vue`
- Create: `frontend/src/composables/useInstall.js`
- Create: `frontend/test/pages/FavoritesPage.spec.js`
- Create: `frontend/test/components/InstallProgressDialog.spec.js`

**Interfaces:**
- Produces: `useInstall()` with `install`, `pause`, `resume`, `cancel`, `phase`, `progress`, `error`, and `dispose`.
- Consumes: Wails install events and existing install methods.

- [ ] **Step 1: Write failing install lifecycle tests**

Cover event registration and cleanup, start, progress, pause, resume, cancel, completion, error, and prevention of accidental close during an active mutation.

- [ ] **Step 2: Implement `useInstall.js`**

Centralize event listeners. `dispose()` must unregister every listener exactly once.

- [ ] **Step 3: Build Favorites and version management**

Use a consistent hierarchy for tool identity, active version, local source, installed versions, available versions, and quick actions.

- [ ] **Step 4: Build the detail drawer**

Use `min(520px, calc(100vw - 24px))`; provide near-full-width behavior on narrow windows and return focus after close.

- [ ] **Step 5: Build installation progress**

Keep pause, resume, cancel, phase, transferred bytes, speed, and technical error details. Avoid color-only phase communication.

- [ ] **Step 6: Remove old implementations and verify**

Run:

```powershell
npm run test:unit -- FavoritesPage.spec.js InstallProgressDialog.spec.js
npm run build
npm run test:e2e
```

- [ ] **Step 7: Commit the migration**

```powershell
git add frontend/src/pages/FavoritesPage.vue frontend/src/components/ToolDetailDrawer.vue frontend/src/components/VersionList.vue frontend/src/components/InstallProgressDialog.vue frontend/src/composables/useInstall.js frontend/src/AppContent.vue frontend/test
git commit -m "feat: redesign tool lifecycle views"
```

---

## Task 7: Redesign Setup Wizard

**Files:**
- Modify: `frontend/src/SetupWizard.vue`
- Modify: `frontend/src/i18n/locales/zh.ts`
- Modify: `frontend/src/i18n/locales/en.ts`
- Create: `frontend/src/components/setup/SetupProgress.vue`
- Create: `frontend/src/components/setup/DriveOption.vue`
- Create: `frontend/test/SetupWizard.spec.js`

**Interfaces:**
- Produces: accessible drive selection and the existing `done` event.
- Consumes: `GetDrives`, `GetLastSelection`, `SelectDirectory`, and `InitConfig`.

- [ ] **Step 1: Write failing keyboard and responsive tests**

Test arrow/space/enter selection, restored selections, custom path selection, back/next flow, save errors, completion, and width at `1024x640`.

- [ ] **Step 2: Extract progress and drive option components**

Use a real radio group or equivalent native keyboard semantics. Explain system-drive risk and recommended-drive criteria in text.

- [ ] **Step 3: Redesign the wizard layout**

Use `width: min(640px, calc(100vw - 32px))`, allow long paths to wrap or truncate safely, and use the semantic success token.

- [ ] **Step 4: Move all error text to i18n**

Replace `Save failed` and all newly introduced strings in both locale files.

- [ ] **Step 5: Verify Setup Wizard**

Run:

```powershell
npm run test:unit -- SetupWizard.spec.js
npm run build
npm run test:e2e
```

- [ ] **Step 6: Commit the wizard**

```powershell
git add frontend/src/SetupWizard.vue frontend/src/components/setup frontend/src/i18n frontend/test/SetupWizard.spec.js
git commit -m "feat: redesign setup wizard"
```

---

## Task 8: Migrate Settings and Environment Variables

**Files:**
- Modify: `frontend/src/AppContent.vue`
- Create: `frontend/src/pages/SettingsPage.vue`
- Create: `frontend/src/pages/EnvironmentVariablesPage.vue`
- Create: `frontend/src/components/settings/ThemeSettings.vue`
- Create: `frontend/src/components/settings/LanguageSettings.vue`
- Create: `frontend/src/components/settings/MirrorSettings.vue`
- Create: `frontend/src/components/settings/ProxySettings.vue`
- Create: `frontend/src/components/settings/CacheSettings.vue`
- Create: `frontend/test/pages/SettingsPage.spec.js`
- Create: `frontend/test/pages/EnvironmentVariablesPage.spec.js`

**Interfaces:**
- Produces: setting sections using shared `FormSection` contracts.
- Consumes: existing theme, locale, mirror, proxy, profile, diagnostics, cache, and environment APIs.

- [ ] **Step 1: Write failing settings behavior tests**

Cover theme persistence, locale, path mode, mirror edit/test/save/remove, proxy enable/test/save, profile import/export, updates, diagnostics, and cache cleanup confirmation.

- [ ] **Step 2: Implement grouped settings navigation**

Use clear sections rather than a continuous stack of equal-weight cards. Preserve all controls.

- [ ] **Step 3: Redesign mirror status presentation**

Display concrete URL verification status, latency, source role, and last checked time. Do not label a mirror healthy based only on its root URL.

- [ ] **Step 4: Write failing environment variable tests**

Cover development/all scopes, level filtering, invalid detection, add/edit/cancel/remove, migration modes, and non-color status labels.

- [ ] **Step 5: Implement responsive environment forms and table**

Stack inputs vertically at narrow widths and preserve action visibility.

- [ ] **Step 6: Remove old markup and verify**

Run:

```powershell
npm run test:unit -- SettingsPage.spec.js EnvironmentVariablesPage.spec.js
npm run build
npm run test:e2e
```

- [ ] **Step 7: Commit settings and environment pages**

```powershell
git add frontend/src/pages/SettingsPage.vue frontend/src/pages/EnvironmentVariablesPage.vue frontend/src/components/settings frontend/src/AppContent.vue frontend/test/pages
git commit -m "feat: redesign settings and environment pages"
```

---

## Task 9: Migrate Project Development Pages

**Files:**
- Modify: `frontend/src/AppContent.vue`
- Create: `frontend/src/pages/ProjectPage.vue`
- Create: `frontend/src/pages/TasksPage.vue`
- Create: `frontend/src/pages/PluginsPage.vue`
- Create: `frontend/src/pages/ServicesPage.vue`
- Create: `frontend/test/pages/ProjectPage.spec.js`
- Create: `frontend/test/pages/TasksPage.spec.js`
- Create: `frontend/test/pages/PluginsPage.spec.js`
- Create: `frontend/test/pages/ServicesPage.spec.js`

**Interfaces:**
- Produces: focused page components using current Wails response models.
- Consumes: project, lockfile, task, plugin, and service APIs.

- [ ] **Step 1: Write failing page tests**

Cover project discovery, tools, environment values, lockfile refresh/install, task execution, plugin types, service refresh/start/stop, loading, empty, and error states.

- [ ] **Step 2: Implement Project and Tasks pages**

Use copyable paths/commands/hashes and consistent information rows. Task states must include text for running, success, and failure.

- [ ] **Step 3: Implement Plugins and Services pages**

Clearly label builtin, Lua, and vfox plugins. Service status must use text and icon in addition to color.

- [ ] **Step 4: Apply confirmations and focus restoration**

Destructive project, plugin, or service operations must use the shared confirmation flow.

- [ ] **Step 5: Remove old markup and verify**

Run:

```powershell
npm run test:unit -- ProjectPage.spec.js TasksPage.spec.js PluginsPage.spec.js ServicesPage.spec.js
npm run build
npm run test:e2e
```

- [ ] **Step 6: Commit project pages**

```powershell
git add frontend/src/pages frontend/src/AppContent.vue frontend/test/pages
git commit -m "feat: redesign project development pages"
```

---

## Task 10: Migrate Advanced and Supporting Pages

**Files:**
- Modify: `frontend/src/AppContent.vue`
- Create: `frontend/src/pages/AIAssistantPage.vue`
- Create: `frontend/src/pages/IsolationPage.vue`
- Create: `frontend/src/pages/RemoteDevelopmentPage.vue`
- Create: `frontend/src/pages/CustomToolsPage.vue`
- Create: `frontend/src/pages/AboutPage.vue`
- Create: `frontend/test/pages/AIAssistantPage.spec.js`
- Create: `frontend/test/pages/IsolationPage.spec.js`
- Create: `frontend/test/pages/RemoteDevelopmentPage.spec.js`
- Create: `frontend/test/pages/CustomToolsPage.spec.js`
- Create: `frontend/test/pages/AboutPage.spec.js`

**Interfaces:**
- Produces: final independent feature pages.
- Consumes: existing AI, isolation, remote, custom tool, and update APIs.

- [ ] **Step 1: Write failing behavior tests for every page**

Cover available/unavailable AI state, suggest/diagnose/chat, isolation provider state and mutation, remote create/connect/destroy, custom tool validation/add/remove, About version, and update entry points.

- [ ] **Step 2: Implement AI, Isolation, and Remote pages**

Use explicit status sections, restrained result surfaces, copyable output, and confirmation for destructive operations.

- [ ] **Step 3: Implement Custom Tools and About pages**

Validate HTTPS URLs and archive format feedback without claiming remote availability before a real test. Preserve all current fields and behavior.

- [ ] **Step 4: Remove old markup and verify**

Run:

```powershell
npm run test:unit -- AIAssistantPage.spec.js IsolationPage.spec.js RemoteDevelopmentPage.spec.js CustomToolsPage.spec.js AboutPage.spec.js
npm run build
npm run test:e2e
```

- [ ] **Step 5: Commit advanced pages**

```powershell
git add frontend/src/pages frontend/src/AppContent.vue frontend/test/pages
git commit -m "feat: redesign advanced feature pages"
```

---

## Task 11: Finish Frontend Consolidation and Accessibility

**Files:**
- Modify: `frontend/src/AppContent.vue`
- Modify: `frontend/src/i18n/index.ts`
- Modify: `frontend/src/i18n/locales/zh.ts`
- Modify: `frontend/src/i18n/locales/en.ts`
- Modify: `frontend/index.html`
- Modify: `frontend/src/styles/theme.css`
- Modify: `frontend/src/components/CommandPalette.vue`
- Modify: `frontend/e2e/accessibility.spec.js`

**Interfaces:**
- Produces: a thin application composition root and complete keyboard/focus behavior.
- Consumes: all page, shell, navigation, shared component, and composable contracts from Tasks 2-10.

- [ ] **Step 1: Write failing final accessibility tests**

Test document language synchronization, visible focus, command palette arrow navigation, Enter, Escape, focus return, dialog/drawer trapping, reduced motion, icon accessible names, and no serious axe violations.

- [ ] **Step 2: Reduce `AppContent.vue` to composition responsibilities**

Keep page registry, top-level selection, and shell assembly. Remove migrated business templates, duplicate state, and stale CSS.

- [ ] **Step 3: Complete Command Palette semantics**

Add listbox/option semantics or equivalent accessible native behavior, arrow-key active selection, Enter activation, Escape close, and trigger focus restoration.

- [ ] **Step 4: Synchronize locale and document language**

Set `document.documentElement.lang` to `zh-CN` or `en` whenever locale changes.

- [ ] **Step 5: Scan style and localization regressions**

Reject undefined `--xdev-*` variables, hard-coded legacy cyan colors, hard-coded user-visible English, and missing translation keys.

- [ ] **Step 6: Run full frontend verification**

```powershell
npm test
npm run test:unit
npm run build
npm run test:e2e
```

- [ ] **Step 7: Commit frontend consolidation**

```powershell
git add frontend
git commit -m "refactor: complete accessible frontend redesign"
```

---

## Task 12: Build the Reproducible Download Audit Tool

**Files:**
- Create: `cmd/linkaudit/main.go`
- Create: `internal/linkaudit/types.go`
- Create: `internal/linkaudit/audit.go`
- Create: `internal/linkaudit/report.go`
- Create: `internal/linkaudit/audit_test.go`
- Create: `docs/download-source-policy.md`

**Interfaces:**
- Produces: `Audit(ctx context.Context, req Request) ([]Result, error)`.
- Produces: `WriteJSON(w io.Writer, results []Result) error`.
- Produces: `WriteMarkdown(w io.Writer, results []Result) error`.
- Consumes: built-in manifest schemas and mirror candidate rules.

```go
type Mode string

const (
    ModeProbe Mode = "probe"
    ModeFull  Mode = "full"
)

type Result struct {
    Tool            string    `json:"tool"`
    Version         string    `json:"version"`
    OS              string    `json:"os"`
    Arch            string    `json:"arch"`
    Role            string    `json:"role"`
    SourceURL       string    `json:"sourceUrl"`
    FinalURL        string    `json:"finalUrl"`
    StatusCode      int       `json:"statusCode"`
    ContentType     string    `json:"contentType"`
    ContentLength   int64     `json:"contentLength"`
    SupportsRange   bool      `json:"supportsRange"`
    ExpectedSHA256  string    `json:"expectedSha256"`
    ActualSHA256    string    `json:"actualSha256"`
    SHA256Matches   bool      `json:"sha256Matches"`
    ArchiveDetected string    `json:"archiveDetected"`
    Available       bool      `json:"available"`
    FailureReason   string    `json:"failureReason"`
    CheckedAt       time.Time `json:"checkedAt"`
}
```

- [ ] **Step 1: Write failing URL behavior tests**

Use `httptest.Server` to test normal download, `206`, HEAD `405` fallback, redirects, downgrade rejection, `404`, `410`, `500`, HTML disguised as an archive, content-type mismatch with valid magic bytes, correct/incorrect SHA, timeout, redirect loop, and same-URL deduplication.

- [ ] **Step 2: Run tests and confirm the package is absent**

Run: `go test ./internal/linkaudit/... -v`

Expected: FAIL because the package and interfaces do not exist.

- [ ] **Step 3: Implement request and result models**

Include manifest directory, platforms, mode, cache directory, request timeout, concurrency limit, and output paths in `Request`.

- [ ] **Step 4: Implement safe probing**

Try HEAD first. If unsupported or inconclusive, send `Range: bytes=0-65535`. Follow safe HTTPS redirects, reject HTTPS-to-HTTP downgrade, and capture the final URL.

- [ ] **Step 5: Implement archive detection and full verification**

Inspect ZIP, gzip, xz, 7z, executable, and script signatures. In full mode, stream to a controlled cache while calculating SHA-256. Never load large archives entirely into memory.

- [ ] **Step 6: Implement deterministic reports**

Sort by tool, version, OS, arch, role, and source URL. Report statuses `PASS`, `URL_DEAD`, `MIRROR_DEAD`, `HASH_MISMATCH`, `HTML_INSTEAD_OF_ARCHIVE`, `UNSUPPORTED_LAYOUT`, `AUTH_OR_LICENSE_REQUIRED`, and `IMPLEMENTATION_MISSING`.

- [ ] **Step 7: Implement the CLI**

Support:

```powershell
go run ./cmd/linkaudit -manifests ./manifests -platform windows/amd64 -mode full -report ./build/download-audit-windows.json
go run ./cmd/linkaudit -manifests ./manifests -platform linux/amd64,darwin/amd64,darwin/arm64 -mode probe -report ./build/download-audit-cross-platform.json
```

- [ ] **Step 8: Document source policy**

Define official source, official release asset, trusted mirror, archive site, unacceptable third-party source, SHA requirements, and release gates.

- [ ] **Step 9: Verify the audit tool**

Run:

```powershell
go test ./internal/linkaudit/... -v
go vet ./internal/linkaudit/... ./cmd/linkaudit/...
```

- [ ] **Step 10: Commit audit tooling**

```powershell
git add cmd/linkaudit internal/linkaudit docs/download-source-policy.md
git commit -m "feat: add manifest download auditor"
```

---

## Task 13: Audit All Windows x64 Built-in Archives

**Files:**
- Read: `manifests/*.yaml`
- Generate: `build/download-audit-windows.json`
- Generate: `build/download-audit-windows.md`
- Create: `docs/download-source-audit.md`

**Interfaces:**
- Produces: one result for every Windows amd64 archive candidate and source role.
- Consumes: `cmd/linkaudit` from Task 12.

- [ ] **Step 1: Enumerate concrete Windows x64 candidates**

Include official archive URL, manifest mirror URL, and default-mirror rewritten URL. Deduplicate network transfers while retaining one report row per manifest entry.

- [ ] **Step 2: Audit small tools first**

Run full verification for small ZIPs/executables to validate the live workflow before large Android, Flutter, LLVM, GHC, and NDK assets.

- [ ] **Step 3: Audit large tools in resumable batches**

Use the controlled cache and preserve partial files. Do not redownload identical URLs referenced by multiple platform entries.

- [ ] **Step 4: Investigate known P0 anomalies**

Explicitly report:

- Android archives sharing `a04cfd774f522d7944520b01a09b3e0fbf4069e9a806cd0c620179b987340029`.
- Git source URLs with conflicting SHA values.
- Kotlin compiler URLs with conflicting SHA values.
- `https://sh.rustup.rs` as a rolling script rather than an archive.
- SourceForge Lua redirects and HTML responses.
- Java entries with empty archives and no working Java plugin.
- Go and Rust default mirror rewrites.
- Android mirror-key normalization failures.

- [ ] **Step 5: Produce the human audit summary**

`docs/download-source-audit.md` must list every failure, concrete evidence, proposed official alternative, proposed trusted mirror, and whether a complete SHA was verified.

- [ ] **Step 6: Review audit completeness**

Compare the report count with the count of Windows amd64 manifest archive entries and generated mirror candidates. Any difference must be explained in the report.

- [ ] **Step 7: Commit only the durable audit summary**

```powershell
git add docs/download-source-audit.md
git commit -m "docs: record Windows download source audit"
```

Generated `build/` reports should be attached as CI artifacts unless the repository explicitly tracks them.

---

## Task 14: Probe Linux and macOS Built-in Archives

**Files:**
- Read: `manifests/*.yaml`
- Generate: `build/download-audit-cross-platform.json`
- Generate: `build/download-audit-cross-platform.md`
- Modify: `docs/download-source-audit.md`

**Interfaces:**
- Produces: reachability and archive-signature evidence for Linux/macOS declarations.
- Consumes: probe mode from Task 12.

- [ ] **Step 1: Run the cross-platform probe**

Cover `linux/amd64`, `linux/arm64`, `darwin/amd64`, and `darwin/arm64` where declared.

- [ ] **Step 2: Perform static shared-URL consistency checks**

If two entries use the same normalized URL, require identical expected SHA values. Report conflicts even though the archive is not fully downloaded.

- [ ] **Step 3: Classify evidence correctly**

Use separate fields for `reachable`, `archiveSignatureValid`, and `fullShaVerified`. Probe mode must never claim full SHA verification.

- [ ] **Step 4: Update the durable audit summary**

Document dead links, redirects, unsupported platform declarations, and archives requiring a later full download before SHA updates.

- [ ] **Step 5: Commit the cross-platform summary**

```powershell
git add docs/download-source-audit.md
git commit -m "docs: add cross-platform source probe results"
```

---

## Task 15: Fix Mirror Candidate Construction and Health Checks

**Files:**
- Modify: `internal/mirror/mirror.go`
- Modify: `internal/mirror/mirror_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/installer/installer.go`
- Modify: relevant installer tests

**Interfaces:**
- Produces: `Candidates(tool string, archive manifest.Archive) []Candidate` with concrete URLs.
- Produces: concrete-archive health status, not root-base status.
- Consumes: manifest official and mirror URLs plus configured mirror rules.

- [ ] **Step 1: Write failing tests for known rewrite errors**

Cover Go SDK versus module proxy, Rustup paths, Android family key normalization, Flutter storage paths, explicit manifest mirror URLs, `404` health failures, and candidate failure aggregation.

- [ ] **Step 2: Run mirror tests and confirm failures**

Run: `go test ./internal/mirror/... ./internal/installer/... -run 'Mirror|Candidate|Fallback' -v`

Expected: current generic rewriting and `<500` health logic fail the new assertions.

- [ ] **Step 3: Replace universal leaf rewriting**

Prefer complete manifest mirror URLs. For configured mirror bases, use explicit per-tool rewrite rules instead of dropping arbitrary path segments.

- [ ] **Step 4: Normalize Android tool families**

Map all Android manifest names to the configured `android` family only for mirror selection; retain the original tool identity in reports and errors.

- [ ] **Step 5: Probe concrete archive candidates**

Only valid 2xx responses, safe redirects, and recognizable archive/script/executable payloads count as healthy. A `404` is unavailable.

- [ ] **Step 6: Aggregate candidate failures**

When all sources fail, return one error containing every attempted URL and a concise reason without leaking credentials or proxy secrets.

- [ ] **Step 7: Verify mirror and installer tests**

```powershell
go test ./internal/mirror/... ./internal/installer/... -v
```

- [ ] **Step 8: Commit mirror fixes**

```powershell
git add internal/mirror internal/config/config.go internal/installer
git commit -m "fix: validate concrete mirror archives"
```

---

## Task 16: Fix Java and Special Installation Kinds

**Files:**
- Modify: `manifests/java.yaml`
- Create or modify: Java plugin files under `internal/plugin/`
- Modify: `internal/manifest/manifest.go`
- Modify: `internal/installer/installer.go`
- Create or modify: related tests under `internal/plugin/` and `internal/installer/`

**Interfaces:**
- Produces: explicit `InstallKind` values `archive`, `executable`, and `script`.
- Produces: a Java `ArchiveFor(version, os, arch)` implementation returning a fixed, reproducible artifact.
- Consumes: installer download, checksum, extraction, and process execution boundaries.

```go
type InstallKind string

const (
    InstallKindArchive    InstallKind = "archive"
    InstallKindExecutable InstallKind = "executable"
    InstallKindScript     InstallKind = "script"
)
```

- [ ] **Step 1: Write failing Java archive resolution tests**

Test Java 17 and 21 on Windows amd64 plus declared Linux/macOS mappings. Assert that every supported tuple resolves to a concrete fixed URL, format, and SHA.

- [ ] **Step 2: Write failing special-kind installer tests**

Assert that executables are not extracted, scripts are not guessed as archives, and scripts require an explicit trusted install path.

- [ ] **Step 3: Select fixed Java distribution assets**

Prefer Eclipse Temurin/Adoptium fixed releases over Oracle `latest` URLs and OTN cookie requirements. Record the decision in `docs/download-source-audit.md`.

- [ ] **Step 4: Implement Java archive resolution**

Return concrete assets for supported versions/platforms and a clear unsupported-platform error otherwise. Do not silently fall back to an unrelated major version.

- [ ] **Step 5: Implement explicit install kinds**

Route archives to extraction, executables to the executable installer flow, and scripts only to an explicit script flow. Preserve checksum verification for every downloaded payload.

- [ ] **Step 6: Replace Rust rolling script behavior**

Use fixed platform-specific `rustup-init` assets or a version-pinned and hash-verified script contract. Do not treat `https://sh.rustup.rs` as an ordinary archive.

- [ ] **Step 7: Verify Java and special installers**

```powershell
go test ./internal/plugin/... ./internal/installer/... -run 'Java|InstallKind|Rust' -v
go vet ./internal/plugin/... ./internal/installer/...
```

- [ ] **Step 8: Commit special installer support**

```powershell
git add manifests/java.yaml internal/plugin internal/manifest internal/installer docs/download-source-audit.md
git commit -m "fix: support reproducible Java and installer kinds"
```

---

## Task 17: Replace Failed Sources and Correct Archive Hashes

**Files:**
- Modify: failed `manifests/*.yaml`
- Modify: corresponding `manifests/*.yaml.sig`
- Modify: `docs/download-source-audit.md`
- Modify: signer implementation/tests if canonical manifest signing is adopted

**Interfaces:**
- Produces: verified official and trusted mirror candidates for each corrected entry.
- Consumes: complete Windows hashes, official checksums, cross-platform probe evidence, and signing tools.

- [ ] **Step 1: Apply fixed source precedence**

Use this order:

1. Official stable archive/CDN.
2. Official GitHub Release asset.
3. Trusted mirror with byte-identical content.
4. Official historical archive site.
5. If no reliable source exists, mark the version unavailable instead of adding an untrusted third-party file.

- [ ] **Step 2: Correct Windows x64 hashes from complete downloads**

Replace copied or mismatched values only when the actual full-file SHA has been recorded by the auditor.

- [ ] **Step 3: Correct cross-platform hashes conservatively**

Use an upstream official checksum when available. Otherwise, perform a full download before changing the hash. Never infer SHA from a probe response.

- [ ] **Step 4: Enforce same-URL hash consistency**

Every normalized identical URL must have the same expected hash in all entries.

- [ ] **Step 5: Re-sign changed manifests**

Regenerate each changed `.yaml.sig`. Do not use `keys/priv.key` as a production key unless the user explicitly confirms it is controlled and intended for production signing.

- [ ] **Step 6: Strengthen signature coverage**

Prefer signing a canonical representation that includes tool, version, OS, arch, URL, mirror URL, SHA, format, and timestamp. Add compatibility only if shipped signatures require it; otherwise replace the incomplete payload directly.

- [ ] **Step 7: Verify corrected manifests**

```powershell
go test ./internal/manifest/... ./internal/signer/... ./internal/downloader/... ./internal/installer/... -v
go run ./cmd/linkaudit -manifests ./manifests -platform windows/amd64 -mode full -report ./build/download-audit-windows-fixed.json
```

Expected: every changed Windows entry is `PASS`, or is intentionally marked unavailable with a documented reason.

- [ ] **Step 8: Commit manifest corrections**

```powershell
git add manifests internal/signer docs/download-source-audit.md
git commit -m "fix: replace invalid built-in download sources"
```

---

## Task 18: Add Download Audit Release Gates

**Files:**
- Modify: `.github/workflows/ci.yaml`
- Modify: `docs/test-plan.md`
- Modify: `docs/operations.md`
- Create or modify: tests under `internal/e2e/`

**Interfaces:**
- Produces: fast per-commit checks, scheduled probe audits, and pre-release Windows full audits.
- Consumes: `cmd/linkaudit` and corrected manifests.

- [ ] **Step 1: Add fast deterministic CI tests**

Run linkaudit, mirror, downloader, signer, manifest, and installer unit tests on every change without live multi-gigabyte downloads.

- [ ] **Step 2: Add a scheduled probe workflow**

Probe all declared platforms daily or manually and upload JSON/Markdown reports as CI artifacts.

- [ ] **Step 3: Add a release-only full Windows audit**

Require full Windows amd64 content and SHA verification before release. Use caching and explicit timeouts.

- [ ] **Step 4: Separate transient and deterministic failures**

DNS/timeouts may be retried and reported as transient. `404`, `410`, invalid archive bytes, and SHA mismatch are deterministic failures and must fail the release gate.

- [ ] **Step 5: Update operational documentation**

Document how to audit a new manifest, choose a mirror, compute hashes, re-sign, inspect reports, and respond to a dead source.

- [ ] **Step 6: Verify CI commands locally**

```powershell
go test ./internal/linkaudit/... ./internal/mirror/... ./internal/downloader/... ./internal/signer/... ./internal/manifest/... ./internal/installer/...
go vet ./...
```

- [ ] **Step 7: Commit release gates**

```powershell
git add .github/workflows/ci.yaml docs/test-plan.md docs/operations.md internal/e2e
git commit -m "ci: gate releases on download source audits"
```

---

## Task 19: Run Integrated GUI Acceptance

**Files:**
- Modify: `frontend/e2e/app-shell.spec.js`
- Modify: `frontend/e2e/accessibility.spec.js`
- Create: `frontend/e2e/core-flows.spec.js`
- Create: `frontend/e2e/visual-regression.spec.js`
- Modify: `docs/test-plan.md`

**Interfaces:**
- Produces: automated evidence for primary GUI flows and the approved visual matrix.
- Consumes: completed frontend and mocked or isolated Wails APIs.

- [ ] **Step 1: Cover the viewport and scale matrix**

Test `1024x640`, `1280x720`, `1440x900`, and `1920x1080`. Manually verify Windows 125%, 150%, and 200% scaling where Playwright cannot accurately emulate native WebView scaling.

- [ ] **Step 2: Cover theme and locale combinations**

Capture dark Chinese, dark English, light Chinese, and light English for Dashboard, catalog, drawer, install progress, Setup, Settings, and representative advanced pages.

- [ ] **Step 3: Cover core product flows**

Automate Setup, navigation, search, official/mirror status display, small-tool installation, pause/resume/cancel, version switching, uninstall, favorites, environment variables, mirror/proxy settings, Command Palette, and window controls.

- [ ] **Step 4: Run accessibility acceptance**

Verify keyboard-only Setup, logical tab order, accessible icon names, Command Palette arrows/Enter/Escape, dialog/drawer focus trapping and restoration, visible focus, non-color statuses, reduced motion, and language synchronization.

- [ ] **Step 5: Run the complete frontend suite**

```powershell
npm test
npm run test:unit
npm run build
npm run test:e2e
```

- [ ] **Step 6: Record manual native-window results**

Update `docs/test-plan.md` with Wails title-bar dragging, minimize, maximize, restore, close, native scale, and long-path behavior results.

- [ ] **Step 7: Commit acceptance coverage**

```powershell
git add frontend/e2e docs/test-plan.md
git commit -m "test: cover redesigned GUI acceptance flows"
```

---

## Task 20: Run Final Product Verification

**Files:**
- Modify only files required to correct failures discovered by this task.
- Produce: final CI and audit artifacts.

**Interfaces:**
- Consumes: all previous tasks.
- Produces: release readiness evidence.

- [ ] **Step 1: Run all frontend checks**

```powershell
npm test
npm run test:unit
npm run build
npm run test:e2e
```

Working directory: `frontend`.

- [ ] **Step 2: Run all Go checks**

```powershell
go vet ./...
go test ./internal/...
go test -tags integration ./internal/integration/...
```

- [ ] **Step 3: Run final download audits**

```powershell
go run ./cmd/linkaudit -manifests ./manifests -platform windows/amd64 -mode full -report ./build/download-audit-windows-final.json
go run ./cmd/linkaudit -manifests ./manifests -platform linux/amd64,linux/arm64,darwin/amd64,darwin/arm64 -mode probe -report ./build/download-audit-cross-platform-final.json
```

- [ ] **Step 4: Build the Wails application**

```powershell
wails build
```

- [ ] **Step 5: Review final artifacts**

Confirm:

- Every existing GUI page is reachable and uses the new shared visual system.
- No neon cyan or undefined theme variables remain.
- Dark/light and Chinese/English combinations pass.
- Windows x64 built-in archives are fully verified or explicitly disabled with a documented reason.
- Linux/macOS entries have truthful probe status and do not claim unperformed SHA verification.
- Official and mirror candidates show concrete per-file results.
- Manifest signatures match all changed manifests and production builds inject the matching public key.

- [ ] **Step 6: Create the final verification commit if fixes were required**

```powershell
git add <only-files-fixed-during-final-verification>
git commit -m "fix: resolve final XDev verification failures"
```

Do not create an empty commit.

---

## Execution Order

1. Complete Tasks 1-2 to establish frontend safety and visual foundations.
2. Complete Task 12 so live audits can run while UI work continues.
3. Complete Tasks 3-11 page-by-page, preserving a working application after every commit.
4. Run Tasks 13-14 in batches while frontend migrations proceed.
5. Complete Tasks 15-18 after audit evidence identifies concrete failures.
6. Complete Tasks 19-20 only after both workstreams pass their focused tests.

## Acceptance Deliverables

- Fully redesigned XDev GUI using neutral gray and indigo themes.
- Dark and light semantic token system shared with Naive UI.
- Focused page components replacing the 3171-line multipage template.
- Responsive desktop behavior and Windows scaling validation.
- Keyboard, focus, reduced-motion, language, and WCAG checks.
- Reproducible Go download audit CLI.
- Complete Windows x64 audit report.
- Linux/macOS reachability and archive-signature report.
- Official and trusted mirror alternatives for every failed built-in source.
- Corrected URLs, mirror rules, archive types, SHA-256 values, and signatures.
- Scheduled source monitoring and release audit gates.

## Plan Self-Review

- **Spec coverage:** Full-page UI redesign, approved visual direction, all declared source classes, Windows-first verification, cross-platform probes, fallback behavior, Java, Rust, mirrors, hashes, signatures, accessibility, and final builds are assigned to explicit tasks.
- **Scope:** UI and download governance remain independent until integrated acceptance, so network work does not block UI delivery.
- **Type consistency:** `useTools`, `useInstall`, navigation registry, `InstallKind`, and linkaudit result/report contracts are defined before consumption.
- **Ambiguity:** A source is not considered usable unless its concrete payload passes the evidence level claimed by the report. Probe mode never implies full SHA verification.
- **Placeholders:** The plan contains no deferred implementation markers; production signing is explicitly gated on authorized key ownership rather than silently using the repository private key.
