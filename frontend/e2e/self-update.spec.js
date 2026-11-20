import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { createInitializedWailsBrowserMocks } from '../test/mocks/wails.js';

const assetName = 'xdev-windows-amd64-v1.0.0-rc.2.exe';
const available = { current: '1.0.0-rc.1', latest: '1.0.0-rc.2', hasUpdate: true, applied: false, ready: false, restarting: false, platform: 'windows/amd64', assetName, downloadUrls: { [assetName]: 'https://example.test/releases/update.exe' } };
const ready = { ...available, ready: true };
const restarting = { ...ready, restarting: true };
async function openApp(page, testInfo, plan = {}, locale = 'en') {
  testInfo.pageErrors = []; testInfo.browserTransfers = [];
  page.on('pageerror', error => testInfo.pageErrors.push(error.message));
  page.on('popup', () => testInfo.browserTransfers.push('popup'));
  page.on('download', () => testInfo.browserTransfers.push('download'));
  page.context().on('request', request => { if (request.url().startsWith('https://example.test/releases/')) testInfo.browserTransfers.push(request.url()); });
  await page.context().route('https://example.test/releases/**', route => route.fulfill({ status: 200, body: 'Executable fixture' }));
  await page.setViewportSize({ width: 1024, height: 760 });
  await page.addInitScript(createInitializedWailsBrowserMocks);
  await page.addInitScript(({ plan, locale }) => {
    localStorage.setItem('xdev-theme', 'dark'); localStorage.setItem('locale', locale);
    window.__wailsSelfUpdatePlan = plan; window.__selfUpdateOpenCalls = [];
    window.open = (...args) => { window.__selfUpdateOpenCalls.push(args); return null; };
  }, { plan, locale });
  await page.goto('/', { waitUntil: 'networkidle' });
  await expect(page.locator('.dashboard-page')).toBeVisible();
}
async function checkFrom(page, view = 'dashboard') {
  if (view === 'settings') await page.locator('[data-view="settings"]').click();
  const trigger = page.locator('[data-action="check-update"]'); await trigger.focus(); await trigger.click(); return trigger;
}
async function emitProgress(page, patch = {}) {
  await page.evaluate(data => window.__wailsEmit('self-update', data), { ...available, action: 'progress', source: 'manual', phase: 'downloading', downloadedBytes: 320, totalBytes: 1000, percent: 32, ...patch });
}
async function accessibleDialog(page) {
  const result = await new AxeBuilder({ page }).include('[data-testid="self-update-dialog"]').analyze();
  expect(result.violations.filter(item => item.impact === 'serious' || item.impact === 'critical')).toEqual([]);
}
async function calls(page, method) { return page.evaluate(method => window.__wailsCalls[method].length, method); }
test.afterEach(async ({ page }, testInfo) => {
  expect(testInfo.pageErrors || []).toEqual([]); expect(testInfo.browserTransfers || []).toEqual([]);
  expect(await page.evaluate(() => window.__selfUpdateOpenCalls || [])).toEqual([]);
  expect(await page.evaluate(() => window.__wailsUnexpected || [])).toEqual([]);
  expect(await page.evaluate(() => window.__wailsCalls?.WindowClose || 0)).toBe(0);
});

test('download verifies and waits for explicit restart without browser transfer or installation claim', async ({ page }, testInfo) => {
  await openApp(page, testInfo, { checks: [available], prepares: [ready], holdPrepare: true, restarts: [restarting] });
  const trigger = await checkFrom(page); const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('windows/amd64'); await expect(dialog).toContainText(assetName);
  await expect(dialog).not.toContainText('ZIP'); await expect(dialog.locator('a')).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Restart and update', exact: true })).toHaveCount(0);
  await accessibleDialog(page);
  await dialog.getByRole('button', { name: 'Download update', exact: true }).click();
  await expect.poll(() => calls(page, 'PrepareUpdate')).toBe(1); expect(await calls(page, 'ApplyUpdate')).toBe(0);
  await emitProgress(page); await expect(dialog.getByRole('progressbar')).toHaveAttribute('value', '32');
  await expect(dialog.locator('[data-testid="self-update-bytes"]')).toHaveText('320 B / 1000 B · 32%');
  for (const phase of ['verifying', 'completed', 'ready']) {
    await emitProgress(page, { phase, downloadedBytes: 1000, percent: 99 });
    await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeDisabled();
    await expect(dialog.locator('[data-action="self-update-restart-now"]')).toHaveCount(0);
  }
  await page.keyboard.press('Escape'); await expect(dialog).toBeVisible();
  await page.evaluate(() => window.__wailsFinishPrepare());
  await expect(dialog.locator('[data-testid="self-update-ready"]')).toBeVisible();
  expect(await calls(page, 'RestartToUpdate')).toBe(0);
  await expect(page.locator('body')).not.toContainText('Updated to');
  await accessibleDialog(page);
  await dialog.getByRole('button', { name: 'Close', exact: true }).click(); await expect(trigger).toBeFocused();
  await trigger.click(); await expect(dialog.locator('[data-testid="self-update-ready"]')).toBeVisible();
  expect(await calls(page, 'CheckForUpdate')).toBe(1); expect(await calls(page, 'PrepareUpdate')).toBe(1);
  await dialog.getByRole('button', { name: 'Restart and update', exact: true }).click();
  await expect.poll(() => calls(page, 'RestartToUpdate')).toBe(1);
  await expect(dialog.locator('[data-testid="self-update-restarting"]')).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeDisabled();
});

test('settings ready state reopens the prepared update instead of redownloading', async ({ page }, testInfo) => {
  await openApp(page, testInfo, { checks: [available], prepares: [ready] }); await checkFrom(page, 'settings');
  const dialog = page.getByRole('dialog'); await dialog.getByRole('button', { name: 'Download update', exact: true }).click();
  await expect(dialog.locator('[data-testid="self-update-ready"]')).toBeVisible();
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(page.locator('[data-testid="settings-self-update"]')).toContainText('ready');
  await page.locator('[data-action="open-self-update"]').click();
  await expect(dialog.getByRole('button', { name: 'Restart and update', exact: true })).toBeEnabled();
  expect(await calls(page, 'PrepareUpdate')).toBe(1);
});

test('network check errors remain explicit and retry discovery', async ({ page }, testInfo) => {
  await openApp(page, testInfo, { checks: [{ ...available, hasUpdate: false, error: 'network offline' }, available] }); await checkFrom(page, 'settings');
  const dialog = page.getByRole('dialog'); await expect(dialog.getByRole('alert')).toContainText('network offline');
  await expect(page.locator('[data-testid="settings-self-update"]')).not.toContainText('Up to date');
  await dialog.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(dialog.getByRole('alert')).toHaveCount(0); expect(await calls(page, 'CheckForUpdate')).toBe(2);
  await expect(dialog.getByRole('button', { name: 'Download update', exact: true })).toBeVisible();
});

test('checksum failure does not offer restart and preparation can retry', async ({ page }, testInfo) => {
  await openApp(page, testInfo, { checks: [available], prepares: [{ ...available, error: 'checksum mismatch' }, ready] }); await checkFrom(page);
  const dialog = page.getByRole('dialog'); await dialog.getByRole('button', { name: 'Download update', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('checksum mismatch'); await emitProgress(page);
  await expect(dialog.getByRole('alert')).toContainText('checksum mismatch');
  await expect(dialog.locator('[data-action="self-update-restart-now"]')).toHaveCount(0);
  await dialog.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(dialog.locator('[data-testid="self-update-ready"]')).toBeVisible(); expect(await calls(page, 'PrepareUpdate')).toBe(2);
});

test('helper startup failure retains ready payload and retries restart without download', async ({ page }, testInfo) => {
  await openApp(page, testInfo, { checks: [available], prepares: [ready], restarts: [{ ...ready, error: 'cannot start helper' }, restarting] }); await checkFrom(page);
  const dialog = page.getByRole('dialog'); await dialog.getByRole('button', { name: 'Download update', exact: true }).click();
  await dialog.getByRole('button', { name: 'Restart and update', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('cannot start helper');
  await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeEnabled();
  await dialog.getByRole('button', { name: 'Restart and update', exact: true }).click();
  await expect(dialog.locator('[data-testid="self-update-restarting"]')).toBeVisible();
  expect(await calls(page, 'PrepareUpdate')).toBe(1); expect(await calls(page, 'RestartToUpdate')).toBe(2);
});

test('invalidated prepared payload requires a fresh check and download', async ({ page }, testInfo) => {
  await openApp(page, testInfo, { checks: [available, available], prepares: [ready, ready], restarts: [{ ...available, error: 'staged file changed' }] }); await checkFrom(page);
  const dialog = page.getByRole('dialog'); await dialog.getByRole('button', { name: 'Download update', exact: true }).click();
  await dialog.getByRole('button', { name: 'Restart and update', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('staged file changed');
  await expect(dialog.locator('[data-action="self-update-restart-now"]')).toHaveCount(0);
  await dialog.getByRole('button', { name: 'Retry', exact: true }).click();
  await dialog.getByRole('button', { name: 'Download update', exact: true }).click();
  await expect(dialog.locator('[data-testid="self-update-ready"]')).toBeVisible(); expect(await calls(page, 'PrepareUpdate')).toBe(2);
});

test('refetched no-update and already-current discovery never claim installation', async ({ page }, testInfo) => {
  const current = { current: available.latest, latest: available.latest, hasUpdate: false, applied: false, ready: false };
  await openApp(page, testInfo, { checks: [available, current], prepares: [current] }); await checkFrom(page);
  const dialog = page.getByRole('dialog'); await dialog.getByRole('button', { name: 'Download update', exact: true }).click();
  await expect(dialog).toContainText('Already up to date. No update was installed.');
  await expect(dialog.locator('[data-action="self-update-restart-now"]')).toHaveCount(0);
  await dialog.getByRole('button', { name: 'Close', exact: true }).click(); await checkFrom(page);
  await expect(dialog).toHaveCount(0); expect(await calls(page, 'RestartToUpdate')).toBe(0);
});

test('focus and Escape behavior remains accessible before and after preparation', async ({ page }, testInfo) => {
  await openApp(page, testInfo, { checks: [available, available], prepares: [ready], holdPrepare: true });
  const trigger = await checkFrom(page); const dialog = page.getByRole('dialog'); const close = dialog.getByRole('button', { name: 'Close', exact: true });
  await expect(close).toBeFocused(); await page.keyboard.press('Shift+Tab');
  await expect(dialog.getByRole('button', { name: 'Download update', exact: true })).toBeFocused();
  await page.keyboard.press('Tab'); await expect(close).toBeFocused(); await page.keyboard.press('Escape');
  await expect(trigger).toBeFocused(); await trigger.click();
  await dialog.getByRole('button', { name: 'Download update', exact: true }).click();
  await page.keyboard.press('Escape'); await expect(dialog).toBeVisible();
  await page.evaluate(() => window.__wailsFinishPrepare());
  await expect(close).toBeFocused(); await accessibleDialog(page);
});

test('startup notification can be ignored while startup ready requires explicit restart', async ({ page }, testInfo) => {
  await openApp(page, testInfo, { restarts: [restarting] }, 'zh');
  await page.evaluate(data => window.__wailsEmit('self-update', { ...data, action: 'notify' }), available);
  const dialog = page.getByRole('dialog'); await expect(dialog.getByRole('button', { name: '下载更新', exact: true })).toBeVisible();
  await dialog.getByRole('button', { name: '忽略并继续', exact: true }).click(); await expect(dialog).toHaveCount(0);
  expect(await page.evaluate(() => window.__wailsCalls.IgnoreSelfUpdate)).toEqual([[available.latest]]);
  await page.evaluate(data => window.__wailsEmit('self-update', { ...data, action: 'ready', source: 'startup' }), ready);
  await expect(dialog.getByRole('button', { name: '重启并更新', exact: true })).toBeVisible(); expect(await calls(page, 'RestartToUpdate')).toBe(0);
  await accessibleDialog(page); await dialog.getByRole('button', { name: '重启并更新', exact: true }).click();
  await expect(dialog.locator('[data-testid="self-update-restarting"]')).toBeVisible();
});

test('startup progress cannot claim ready and rollback errors remain checkable', async ({ page }, testInfo) => {
  await openApp(page, testInfo, { checks: [available] });
  await emitProgress(page, { source: 'startup', phase: 'ready' }); const dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeDisabled();
  await expect(dialog.locator('[data-action="self-update-restart-now"]')).toHaveCount(0);
  await page.evaluate(data => window.__wailsEmit('self-update', { ...data, action: 'applied-error', ready: false, hasUpdate: false, error: 'old executable restored' }), available);
  await expect(dialog.getByRole('alert')).toContainText('old executable restored');
  await dialog.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(dialog.getByRole('button', { name: 'Download update', exact: true })).toBeVisible();
  expect(await calls(page, 'PrepareUpdate')).toBe(0);
});
