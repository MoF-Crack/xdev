import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { createInitializedWailsBrowserMocks, createWailsBrowserMocks } from '../test/mocks/wails.js';

const cases = {
  setup: { viewport: { width: 1024, height: 640 }, theme: 'dark', locale: 'en' },
  dashboard: { viewport: { width: 1440, height: 900 }, theme: 'dark', locale: 'zh' },
  catalog: { viewport: { width: 1024, height: 640 }, theme: 'light', locale: 'en' },
  settings: { viewport: { width: 1440, height: 900 }, theme: 'light', locale: 'zh' },
  drawer: { viewport: { width: 1440, height: 900 }, theme: 'dark', locale: 'en' },
  progress: { viewport: { width: 1024, height: 640 }, theme: 'light', locale: 'zh' },
};

async function openApp(page, testInfo, options, initialized = true) {
  testInfo.pageErrors = [];
  page.on('pageerror', (error) => testInfo.pageErrors.push(error.message));
  await page.setViewportSize(options.viewport);
  await page.addInitScript(({ theme, locale }) => {
    localStorage.setItem('xdev-theme', theme);
    localStorage.setItem('locale', locale);
  }, options);
  await page.addInitScript(initialized ? createInitializedWailsBrowserMocks : createWailsBrowserMocks);
  await page.goto('/');
  await page.addStyleTag({ content: `
    *, *::before, *::after { animation: none !important; transition: none !important; caret-color: transparent !important; }
  ` });
}

// Default view is the dashboard; tests that need the tool catalog must navigate to it explicitly.
async function openToolCatalog(page) {
  await page.locator('[data-view="language"]').click();
  await expect(page.getByText('Node.js', { exact: true })).toBeVisible();
}

async function expectDocumentContained(page) {
  expect(await page.evaluate(() => ({
    documentWidth: document.documentElement.scrollWidth,
    viewportWidth: window.innerWidth,
  }))).toEqual(expect.objectContaining({ documentWidth: await page.evaluate(() => window.innerWidth) }));
}

async function expectInViewport(locator, viewport) {
  const box = await locator.boundingBox();
  expect(box).not.toBeNull();
  expect(box.x).toBeGreaterThanOrEqual(0);
  expect(box.y).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width).toBeLessThanOrEqual(viewport.width);
  expect(box.y + box.height).toBeLessThanOrEqual(viewport.height);
}

async function expectNoSeriousAxeViolations(page) {
  const { violations } = await new AxeBuilder({ page }).analyze();
  const serious = violations.filter(({ impact }) => impact === 'critical' || impact === 'serious');
  expect(serious).toEqual([]);
  return serious;
}

async function expectVisibleStateAccessible(page, include = 'main') {
  const results = await new AxeBuilder({ page }).include(include).analyze();
  expect(results.violations.filter(({ impact }) => impact === 'critical' || impact === 'serious')).toEqual([]);
}

async function expectVisibleDialogAccessible(page) {
  const dialog = page.getByRole('dialog').last();
  await expect(dialog).toBeVisible();
  const results = await new AxeBuilder({ page }).include('[role="dialog"]').analyze();
  expect(results.violations.filter(({ impact }) => impact === 'critical' || impact === 'serious')).toEqual([]);
}

test.afterEach(async ({}, testInfo) => {
  expect(testInfo.pageErrors ?? []).toEqual([]);
});

test('setup wizard visual baseline', async ({ page }, testInfo) => {
  await openApp(page, testInfo, cases.setup, false);
  await expect(page.locator('.setup-card')).toBeVisible();
  await expectDocumentContained(page);
  const setupBox = await page.locator('.setup-card').boundingBox();
  expect(setupBox.x).toBeGreaterThanOrEqual(0);
  expect(setupBox.x + setupBox.width).toBeLessThanOrEqual(cases.setup.viewport.width);
  expect(setupBox.y).toBeGreaterThanOrEqual(0);
  await page.locator('.setup-nav').scrollIntoViewIfNeeded();
  await expect(page.locator('.setup-nav')).toBeVisible();
  expect(await page.locator('.setup-card').evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
  await expect(page.locator('.setup-card')).toHaveScreenshot('setup-wizard.png', { animations: 'disabled' });
});

test('dashboard visual baseline', async ({ page }, testInfo) => {
  await openApp(page, testInfo, cases.dashboard);
  await page.locator('[data-view="dashboard"]').click();
  await expect(page.getByText('已安装工具', { exact: true })).toBeVisible();
  await expectDocumentContained(page);
  await expectInViewport(page.locator('.app-shell__content'), cases.dashboard.viewport);
  await expect(page.locator('.metric-card')).toHaveCount(4);
  for (const metric of await page.locator('.metric-card').all()) await expect(metric).toBeVisible();
  await expect(page.getByText('路径', { exact: true })).toBeVisible();
  await expect(page.getByText('快捷操作', { exact: true })).toBeVisible();
  await expect(page.getByText('已安装工具', { exact: true })).toBeVisible();
  const dashboardAxe = await expectNoSeriousAxeViolations(page);
  expect(dashboardAxe.some(({ id }) => id === 'color-contrast')).toBe(false);
  await expect(page).toHaveScreenshot('dashboard-1440-dark-zh.png', { animations: 'disabled' });
});

test('tool catalog visual baseline', async ({ page }, testInfo) => {
  await openApp(page, testInfo, cases.catalog);
  await openToolCatalog(page);
  await expect(page.getByText('Node.js', { exact: true })).toBeVisible();
  await expectDocumentContained(page);
  await expectInViewport(page.locator('.app-shell__content'), cases.catalog.viewport);
  await expect(page.locator('[data-testid="tool-search"]')).toBeVisible();
  await expect(page.locator('[data-testid="hide-unavailable"]')).toBeVisible();
  await expect(page.getByText('Node.js', { exact: true })).toBeVisible();
  await expect(page.getByText('20.18.0', { exact: true }).first()).toBeVisible();
  await expect(page.locator('[data-action="detail"]').first()).toBeVisible();
  await expect(page.locator('[data-action="favorite"]').first()).toBeVisible();
  const catalogOverflow = await page.locator('.tool-catalog *').evaluateAll((elements) => elements.filter((element) => {
    const box = element.getBoundingClientRect();
    const scrollContainer = element.closest('.responsive-data-table');
    return !scrollContainer && (box.left < 0 || box.right > window.innerWidth);
  }).map((element) => element.outerHTML.slice(0, 120)));
  expect(catalogOverflow).toEqual([]);
  await expect(page).toHaveScreenshot('catalog-1024-light-en.png', { animations: 'disabled' });
});

test('package manager and favorite actions reach the correct Wails APIs', async ({ page }, testInfo) => {
  await openApp(page, testInfo, cases.catalog);
  await page.locator('[data-view="package-manager"]').click();
  await page.locator('[data-testid="hide-unavailable"]').uncheck();
  await expect(page.getByText('Install from vendor site', { exact: true })).toBeVisible();
  await expect(page.locator('[data-action="package-manager"]')).toHaveCount(0);
  await page.locator('[data-action="detail"][data-tool="npm"]').click();
  await expect(page.locator('[data-action="pm-actions"]')).toBeVisible();
  await page.locator('[data-action="package-manager"]').click();
  await expectVisibleDialogAccessible(page);
  await page.getByRole('dialog').last().getByRole('button', { name: 'Install', exact: true }).click();
  await expect.poll(() => page.evaluate(() => window.__wailsCalls.InstallPackageManager)).toBe(1);
  await page.keyboard.press('Escape');
  await expect(page.locator('.n-drawer-mask')).toHaveCount(0);
  await page.locator('[data-action="detail"][data-tool="pnpm"]').click();
  await expect(page.locator('[data-action="package-manager-update"]')).toBeVisible();
  await page.locator('[data-action="package-manager-update"]').click();
  await expect.poll(() => page.evaluate(() => window.__wailsCalls.UpdatePackageManager)).toBe(1);
  await page.keyboard.press('Escape');
  await expect(page.locator('.n-drawer-mask')).toHaveCount(0);
  await page.getByRole('row').filter({ has: page.getByText('npm', { exact: true }) }).getByRole('button', { name: 'Favorite', exact: true }).click();
  await expect.poll(() => page.evaluate(() => window.__wailsCalls.AddFavorite)).toBe(1);
});

test('settings visual baseline', async ({ page }, testInfo) => {
  await openApp(page, testInfo, cases.settings);
  await page.getByText('设置', { exact: true }).last().click();
  await expect(page.getByRole('group', { name: '语言' })).toBeVisible();
  await expectDocumentContained(page);
  await expectInViewport(page.locator('.app-shell__content'), cases.settings.viewport);
  expect(await page.locator('.app-shell__content').evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
  await expect(page.getByRole('button', { name: '保存镜像' })).toBeVisible();
  await expectNoSeriousAxeViolations(page);
  await expect(page).toHaveScreenshot('settings-1440-light-zh.png', { animations: 'disabled' });
});

test('settings actions reach Wails APIs and destructive cache requires confirmation', async ({ page }, testInfo) => {
  await openApp(page, testInfo, { viewport: { width: 1440, height: 900 }, theme: 'light', locale: 'en' });
  await page.getByText('Settings', { exact: true }).last().click();
  await page.locator('[data-action="theme-dark"]').check();
  await page.locator('[data-action="locale"]').selectOption('zh');
  await page.locator('[data-action="path-mode"]').selectOption('direct');
  await page.locator('[data-action="mirror-test"]').first().click();
  await page.locator('[data-action="mirror-save"]').click();
  await page.locator('[data-action="proxy-enable"]').check();
  await page.getByLabel('HTTP proxy').fill('http://127.0.0.1:8080');
  await page.locator('[data-action="proxy-test"]').click();
  await page.locator('[data-action="proxy-save"]').click();
  await page.locator('[data-action="run-diagnostics"]').click();
  await page.locator('[data-action="cache-clean-all"]').click();
  expect(await page.evaluate(() => window.__wailsCalls.CleanCache)).toEqual([]);
  await page.locator('[role="dialog"] [data-action="confirm"]').click();
  expect(await page.evaluate(() => window.__wailsCalls)).toMatchObject({ SetPathMode: [['direct']], TestMirrorConnectivity: [['https://nodejs.org/dist']], SetProxyConfig: [['http://127.0.0.1:8080', '', '', true]], RunDiagnostics: 1, CleanCache: [['all']] });
  expect((await new AxeBuilder({ page }).include('.settings-page').analyze()).violations.filter(v => ['serious', 'critical'].includes(v.impact))).toEqual([]);
});

test('environment actions and migration reach Wails APIs', async ({ page }, testInfo) => {
  await openApp(page, testInfo, { viewport: { width: 1024, height: 720 }, theme: 'dark', locale: 'en' });
  await page.getByText('Env Vars', { exact: true }).last().click();
  await page.setViewportSize({ width: 640, height: 720 });
  await page.locator('[data-action="all-level"]').selectOption('user');
  await page.locator('[data-action="validate"]').click();
  // The validation dialog appears when invalid variables exist; close it before continuing.
  await page.locator('[role="dialog"] .n-dialog__action').getByRole('button', { name: 'Close' }).click();
  await page.locator('[data-action="new-env"]').click();
  await page.getByLabel('Variable Name').fill('JAVA_HOME');
  await page.getByLabel('Variable Value 1').fill('C:\\Java');
  await page.getByRole('button', { name: 'Save' }).click();
  await page.locator('.env-card[data-row="OLD_HOME"] [data-action="edit-env"]').click();
  await page.getByRole('button', { name: 'Cancel' }).click();
  await page.locator('.env-card[data-row="OLD_HOME"] [data-action="delete-env"]').click();
  await page.locator('[role="dialog"] [data-action="confirm"]').click();
  await page.locator('[data-action="migration-scan"]').click();
  await page.locator('[data-action="migration-single"]').click();
  await page.locator('[data-action="migration-scan"]').click();
  await page.locator('[data-action="migration-all"]').click();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect(await page.evaluate(() => window.__wailsCalls)).toMatchObject({ SetEnvVar: [['', 'JAVA_HOME', 'C:\\Java', 'user']], DeleteEnvVarEntry: [['OLD_HOME', 'user']], ScanMigration: 2, MigrateSingle: expect.any(Array) });
  expect((await new AxeBuilder({ page }).include('.env-page').analyze()).violations.filter(v => ['serious', 'critical'].includes(v.impact))).toEqual([]);
});

test('isolation, remote, and about pages preserve API contracts', async ({ page }, testInfo) => {
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
  await openApp(page, testInfo, { viewport: { width: 640, height: 720 }, theme: 'dark', locale: 'en' });
  await page.locator('[data-view="isolate"]').click(); await expectVisibleStateAccessible(page); await page.locator('[data-action="setup"]').click(); await page.locator('[data-field="isolate-command"]').fill('node --version'); await page.locator('[data-action="exec"]').click(); await page.locator('[data-action="teardown"]').click(); expect(await page.evaluate(() => window.__wailsCalls.IsolateTeardown)).toEqual([]); await expectVisibleDialogAccessible(page); await page.locator('[role="dialog"] [data-action="confirm"]').click();
  await page.locator('[data-view="remote"]').click(); await expectVisibleStateAccessible(page); await page.locator('[data-field="remote-image"]').fill('ubuntu:22.04'); await page.locator('[data-action="create-remote"]').click(); await page.locator('[data-action="copy-address"]').click(); expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('C:\\remote'); await page.locator('[data-field="remote-command"]').fill('pwd'); await page.locator('[data-action="exec-remote"]').click(); await page.locator('[data-action="destroy-remote"]').click(); expect(await page.evaluate(() => window.__wailsCalls.DestroyRemoteInstance)).toEqual([]); await expectVisibleDialogAccessible(page); await page.locator('[role="dialog"] [data-action="confirm"]').click();
  await page.locator('[data-view="about"]').click(); await expect(page.getByText('1.0.0', { exact: true })).toBeVisible(); await expectVisibleStateAccessible(page);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect((await new AxeBuilder({ page }).include('main').analyze()).violations.filter(v => ['serious', 'critical'].includes(v.impact))).toEqual([]);
  expect(await page.evaluate(() => window.__wailsCalls)).toMatchObject({ IsolateSetup: [['container']], IsolateExec: [[['node', '--version']]], IsolateTeardown: [[]], CreateRemoteInstance: [['docker', 'ubuntu:22.04', '', 22, 'root', '', '']], ExecRemoteInstance: [['docker', 'remote-1', ['pwd']]], DestroyRemoteInstance: [['docker', 'remote-1']] });
});

test('tool detail drawer visual baseline', async ({ page }, testInfo) => {
  await openApp(page, testInfo, cases.drawer);
  await openToolCatalog(page);
  await page.getByText('Node.js', { exact: true }).click();
  await expect(page.locator('.n-drawer')).toBeVisible();
  await expectDocumentContained(page);
  const drawerPanel = page.locator('.n-drawer-content-wrapper');
  await expectInViewport(drawerPanel, cases.drawer.viewport);
  expect(await drawerPanel.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
  await expect(page.getByRole('button', { name: 'Install' }).first()).toBeVisible();
  await expect(page).toHaveScreenshot('tool-drawer-1440-dark-en.png', { animations: 'disabled' });
});

test('spec-023 form badges, dependencies, launch and copy-command actions', async ({ page }, testInfo) => {
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
  await openApp(page, testInfo, cases.drawer);
  await openToolCatalog(page);
  // form-factor badges on the catalog table row
  const nodeRow = page.getByRole('row').filter({ has: page.getByText('Node.js', { exact: true }) });
  await expect(nodeRow.locator('[data-form="gui"]')).toHaveText('GUI');
  await expect(nodeRow.locator('[data-form="cli"]')).toHaveText('CLI');
  // open the drawer: dependency row + launch/copy actions
  await nodeRow.locator('[data-action="detail"]').click();
  await expect(page.locator('[data-action="tool-deps"]')).toBeVisible();
  await expect(page.locator('[data-dep="npm"]')).toContainText('Satisfied');
  await expect(page.getByRole('button', { name: 'Launch' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Copy command' })).toBeVisible();
  // copy-command writes the CLI shim to the clipboard
  await page.getByRole('button', { name: 'Copy command' }).click();
  await expect.poll(() => page.evaluate(() => window.__wailsCalls.ToolCommand)).toEqual([['node']]);
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe('node');
  // launch reaches the Wails API
  await page.getByRole('button', { name: 'Launch' }).click();
  await expect.poll(() => page.evaluate(() => window.__wailsCalls.LaunchTool)).toEqual([['node']]);
  // CLI-only tool: no launch button
  await page.keyboard.press('Escape');
  await page.getByRole('row').filter({ has: page.getByText('Go', { exact: true }) }).locator('[data-action="detail"]').click();
  await expect(page.getByRole('button', { name: 'Launch' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Copy command' })).toBeVisible();
});

test('tool detail drawer returns focus to its detail trigger', async ({ page }, testInfo) => {
  await openApp(page, testInfo, cases.drawer);
  await openToolCatalog(page);
  const trigger = page.locator('[data-action="detail"]').first();
  await trigger.focus();
  await trigger.click();
  await expect(page.locator('.n-drawer')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.locator('.n-drawer')).toBeHidden();
  await expect(trigger).toBeFocused();
});

test('command palette supports keyboard navigation and restores trigger focus', async ({ page }, testInfo) => {
  await openApp(page, testInfo, cases.catalog);
  const trigger = page.getByRole('button', { name: 'Open command palette' });
  await trigger.focus();
  await page.keyboard.press('Control+K');

  const combobox = page.getByRole('combobox', { name: 'Search commands, pages, and tools' });
  await expect(combobox).toBeFocused();
  await expectVisibleDialogAccessible(page);
  await expect(combobox).toHaveAttribute('aria-activedescendant', /command-option-/);
  await page.keyboard.press('End');
  const activeId = await combobox.getAttribute('aria-activedescendant');
  await expect(page.locator(`#${activeId}`)).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press('Escape');

  await expect(page.getByRole('dialog', { name: 'Command palette' })).toBeHidden();
  await expect(trigger).toBeFocused();
});

test('installation progress visual baseline', async ({ page }, testInfo) => {
  await openApp(page, testInfo, cases.progress);
  await openToolCatalog(page);
  await page.getByText('Node.js', { exact: true }).click();
  await page.getByRole('button', { name: '安装' }).locator(':not([disabled])').first().click();
  await page.evaluate(() => window.__wailsEmit('installProgress', {
    tool: 'node', version: '22.14.0', percent: 42, phase: 'Downloading', speed: '12 MB/s', eta: '8s',
  }));
  await expect(page.locator('.n-modal')).toBeVisible();
  await expectDocumentContained(page);
  await expectInViewport(page.locator('.n-modal'), cases.progress.viewport);
  expect(await page.locator('.n-modal').evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
  await expect(page.getByRole('button', { name: '暂停' })).toBeVisible();
  await expect(page.getByRole('button', { name: '继续' })).toBeVisible();
  await expect(page.getByRole('button', { name: '取消' })).toBeVisible();
  expect(await page.locator('.n-modal').evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
  const progressAxe = await expectNoSeriousAxeViolations(page);
  expect(progressAxe.some(({ id }) => id === 'color-contrast')).toBe(false);
  await expect(page).toHaveScreenshot('install-progress-1024-light-zh.png', {
    animations: 'disabled',
    maxDiffPixels: 20,
  });
});

test('backgrounded install shows a toolbar progress indicator that reopens the dialog', async ({ page }, testInfo) => {
  await openApp(page, testInfo, cases.progress);
  await openToolCatalog(page);
  await page.getByText('Node.js', { exact: true }).click();
  await page.getByRole('button', { name: '安装' }).locator(':not([disabled])').first().click();
  await page.evaluate(() => window.__wailsEmit('installProgress', {
    tool: 'node', version: '22.14.0', percent: 42, phase: 'Downloading', speed: '12 MB/s', eta: '8s',
  }));
  await expect(page.locator('.n-modal')).toBeVisible();
  // Send the install to the background — the modal closes but stays active.
  await page.getByRole('button', { name: '后台运行' }).click();
  await expect(page.locator('.n-modal')).toBeHidden();
  // The toolbar indicator appears with the live percent.
  const indicator = page.locator('[data-action="show-install-progress"]');
  await expect(indicator).toBeVisible();
  await expect(indicator).toContainText('42%');
  // Live progress keeps flowing into the indicator while backgrounded.
  await page.evaluate(() => window.__wailsEmit('installProgress', {
    tool: 'node', version: '22.14.0', percent: 77, phase: 'Downloading', speed: '9 MB/s', eta: '3s',
  }));
  await expect(indicator).toContainText('77%');
  // Clicking the indicator reopens the progress dialog with live state.
  await indicator.click();
  await expect(page.locator('.n-modal')).toBeVisible();
  await expect(page.locator('.n-modal')).toContainText('77%');
  // The indicator hides while the dialog is open again.
  await expect(indicator).toBeHidden();
});

test('installation pause resume and cancel use the active task and protect the dialog', async ({ page }, testInfo) => {
  await openApp(page, testInfo, cases.progress);
  await openToolCatalog(page);
  await expectVisibleStateAccessible(page, '.tool-catalog');
  const detailTrigger = page.locator('[data-action="detail"]').first();
  await detailTrigger.click();
  await expectVisibleStateAccessible(page, '.n-drawer');
  await page.getByRole('button', { name: '安装' }).first().click();
  await expect(page.locator('.n-modal')).toBeVisible();
  await expectVisibleDialogAccessible(page);
  await page.keyboard.press('Escape');
  await expect(page.locator('.n-modal')).toBeVisible();
  await page.locator('.n-modal-mask').click({ position: { x: 2, y: 2 } });
  await expect(page.locator('.n-modal')).toBeVisible();
  await page.getByRole('button', { name: '暂停' }).click();
  await expect.poll(() => page.evaluate(() => window.__wailsCalls.PauseDownload)).toEqual([['node@22.14.0']]);
  await expect(page.getByRole('button', { name: '继续' })).toBeEnabled();
  await page.getByRole('button', { name: '继续' }).click();
  await expect.poll(() => page.evaluate(() => window.__wailsCalls.ResumeDownload)).toEqual([['node@22.14.0']]);
  await page.getByRole('button', { name: '取消' }).click();
  await expect.poll(() => page.evaluate(() => window.__wailsCalls.CancelDownload)).toEqual([['node@22.14.0']]);
  await expect(page.locator('.n-modal')).toBeHidden();
  await expect(detailTrigger).toBeFocused();
});

// Spec 037 阶段 1 e2e 基线：批量「全部更新」并行执行——多项进度框渲染
// 每项独立行（tool@version + 各自进度 + 逐项取消），确认对话框逐项串行，
// 全部完成后真实汇总。后端 mock 的 InstallToolConfirmed 记录启动调用。
test('batch update runs items in parallel with a multi-item progress dialog', async ({ page }, testInfo) => {
  // Inject the updates list BEFORE the app mounts (init scripts run in
  // order; this one must precede openApp's goto).
  await page.addInitScript(() => {
    window.__wailsUpdates = [
      { tool: 'node', currentVersion: '20.18.0', latestVersion: '22.14.0', command: 'xdev install node@22.14.0' },
      { tool: 'go', currentVersion: '1.24.0', latestVersion: '1.26.0', command: 'xdev install go@1.26.0' },
    ];
  });
  await openApp(page, testInfo, cases.dashboard);

  // The dashboard's install-all button (enabled once updates load).
  const installAll = page.locator('[data-action="install-all-updates"]');
  await expect(installAll).toBeEnabled();
  await installAll.click();
  // Batch confirm dialog (non-unverified mock → no per-item dialogs).
  await page.getByRole('button', { name: /更新|Update/i }).last().click();

  // Both items started in parallel — the two sessions each called
  // InstallToolConfirmed before either Completed event arrives.
  await page.evaluate(() => window.__wailsEmit('installProgress', {
    taskId: 'node@22.14.0', tool: 'node', version: '22.14.0', phase: 'Downloading', percent: 40,
  }));
  await page.evaluate(() => window.__wailsEmit('installProgress', {
    taskId: 'go@1.26.0', tool: 'go', version: '1.26.0', phase: 'Downloading', percent: 60,
  }));

  // Multi-item dialog: one row per item with independent progress.
  // (The Naive confirm dialog also uses .n-modal while animating out —
  // anchor on the install dialog's batch rows instead.)
  const modal = page.locator('.n-modal.n-modal--wide');
  await expect(modal).toBeVisible();
  await expect(modal.locator('[data-batch-item="node"]')).toContainText('40%');
  await expect(modal.locator('[data-batch-item="go"]')).toContainText('60%');
  await expect(page.getByRole('button', { name: /取消 node|Cancel node/i })).toBeVisible();
  await expect(page.getByRole('button', { name: /取消 go|Cancel go/i })).toBeVisible();

  // The parallel property: both installs launched before any completion.
  expect(await page.evaluate(() => window.__wailsCalls.InstallToolConfirmed.length)).toBe(2);

  // Complete both — the batch resolves, reports the truthful summary,
  // and the finally net closes the progress dialog.
  await page.evaluate(() => window.__wailsEmit('installProgress', {
    taskId: 'node@22.14.0', tool: 'node', version: '22.14.0', phase: 'Completed', percent: 100,
  }));
  await page.evaluate(() => window.__wailsEmit('installProgress', {
    taskId: 'go@1.26.0', tool: 'go', version: '1.26.0', phase: 'Completed', percent: 100,
  }));
  await expect(modal).toBeHidden();
});

test('narrow app shell is compact, contained, and has no serious accessibility violations', async ({ page }, testInfo) => {
  await openApp(page, testInfo, { viewport: { width: 640, height: 720 }, theme: 'dark', locale: 'en' });
  await expect(page.getByText('Node.js', { exact: true })).toBeVisible();

  await expect(page.locator('.app-sidebar')).toHaveCSS('width', '56px');
  await expect(page.getByRole('button', { name: 'Expand sidebar' })).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);

  const results = await new AxeBuilder({ page })
    .include('.title-bar')
    .include('.app-sidebar')
    .analyze();
  expect(results.violations.filter(({ impact }) => impact === 'critical' || impact === 'serious')).toEqual([]);
});

test('setup page has no new detectable accessibility violations', async ({ page }, testInfo) => {
  await openApp(page, testInfo, cases.setup, false);
  await expect(page.locator('.setup-card')).toBeVisible();
  const results = await new AxeBuilder({ page })
    .analyze();
  expect(results.violations).toEqual([]);
});

async function expectNoAxeViolations(page) {
  const results = await new AxeBuilder({ page }).analyze();
  expect(results.violations).toEqual([]);
}

test('setup radios support label click, Space, Arrow keys, step flow, and focus', async ({ page }, testInfo) => {
  await openApp(page, testInfo, cases.setup, false);
  const systemDrive = page.getByRole('radio', { name: /C:/ });
  const recommendedDrive = page.getByRole('radio', { name: /D:/ });
  await expect(recommendedDrive).toBeChecked();
  await systemDrive.locator('xpath=ancestor::label').click({ position: { x: 200, y: 20 } });
  await expect(systemDrive).toBeChecked();
  await recommendedDrive.focus();
  await page.keyboard.press('Space');
  await expect(recommendedDrive).toBeChecked();
  await page.keyboard.press('ArrowLeft');
  await expect(systemDrive).toBeChecked();
  await page.getByRole('button', { name: 'Next' }).click();
  await expect(page.getByRole('heading', { name: 'Confirm Configuration' })).toBeFocused();
  await expectNoAxeViolations(page);
  await page.getByRole('button', { name: 'Back' }).click();
  await expect(page.locator('[data-step-heading="1"]')).toBeFocused();
  await page.getByRole('button', { name: 'Next' }).click();
  await page.getByRole('button', { name: 'Confirm & Save' }).click();
  await expect(page.getByRole('heading', { name: 'Setup Complete!' })).toBeFocused();
  await expectNoAxeViolations(page);
  await page.getByRole('button', { name: 'Start Using' }).click();
  await expect.poll(() => page.evaluate(() => window.__wailsCalls.InitConfig)).toEqual([['C:\\XDev']]);
});

test('setup normal, error, and narrow custom path states remain accessible and contained', async ({ page }, testInfo) => {
  await openApp(page, testInfo, { ...cases.setup, viewport: { width: 320, height: 400 } }, false);
  await expectNoAxeViolations(page);
  await page.getByRole('button', { name: 'Browse for installation path' }).click();
  await expect(page.locator('[data-testid="selected-path"]')).toContainText('server');
  await expectDocumentContained(page);
  await page.getByRole('button', { name: 'Next' }).scrollIntoViewIfNeeded();
  await expect(page.getByRole('button', { name: 'Next' })).toBeVisible();
  await expectNoAxeViolations(page);

  await page.addInitScript(() => { window.__failSetupDrives = true; });
  await page.reload();
  await expect(page.locator('#setup-load-error')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Retry' })).toBeVisible();
  await expectNoAxeViolations(page);
  await page.getByRole('button', { name: 'Retry' }).click();
  await expect(page.locator('#setup-load-error')).toBeFocused();
  await page.evaluate(() => { window.__failSetupDrives = false; });
  await page.getByRole('button', { name: 'Retry' }).click();
  await expect(page.getByRole('radio', { name: /C:/ })).toBeFocused();
});
