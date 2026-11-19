import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { createInitializedWailsBrowserMocks } from '../test/mocks/wails.js';

const available = {
  current: '1.0.0-alpha.3', latest: '1.0.0-beta.2', hasUpdate: true, applied: false,
  downloadUrls: { 'xdev-windows-amd64-v1.0.0-beta.2.zip': 'https://example.test/releases/beta.2.zip' },
};
const installed = { ...available, applied: true };
async function openApp(page, testInfo, plan = {}, locale = 'en') {
  testInfo.pageErrors = [];
  page.on('pageerror', error => testInfo.pageErrors.push(error.message));
  await page.setViewportSize({ width: 1024, height: 760 });
  await page.addInitScript(createInitializedWailsBrowserMocks);
  await page.addInitScript(({ plan, locale }) => {
    localStorage.setItem('xdev-theme', 'dark');
    localStorage.setItem('locale', locale);
    window.__wailsSelfUpdatePlan = plan;
  }, { plan, locale });
  await page.goto('/', { waitUntil: 'networkidle' });
  await expect(page.locator('.dashboard-page')).toBeVisible();
}
async function checkFrom(page, view = 'dashboard') {
  if (view === 'settings') await page.locator('[data-view="settings"]').click();
  const trigger = page.locator('[data-action="check-update"]');
  await trigger.focus(); await trigger.click();
  return trigger;
}
async function emitProgress(page, patch = {}) {
  await page.evaluate(data => window.__wailsEmit('self-update', data), {
    action: 'progress', source: 'manual', current: available.current, latest: available.latest,
    phase: 'downloading', downloadedBytes: 320, totalBytes: 1000, percent: 32, ...patch,
  });
}
async function accessibleDialog(page) {
  const result = await new AxeBuilder({ page }).include('[data-testid="self-update-dialog"]').analyze();
  expect(result.violations.filter(item => item.impact === 'serious' || item.impact === 'critical')).toEqual([]);
}
test.afterEach(async ({ page }, testInfo) => {
  expect(testInfo.pageErrors || []).toEqual([]);
  expect(await page.evaluate(() => window.__wailsUnexpected || [])).toEqual([]);
});

test('manual check confirms download/install, real progress, busy protection and restart-only success', async ({ page }, testInfo) => {
  await openApp(page, testInfo, { checks: [available], applies: [installed], holdApply: true });
  const trigger = await checkFrom(page);
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText(available.current); await expect(dialog).toContainText(available.latest);
  await expect(dialog.getByRole('button', { name: 'Download and install', exact: true })).toBeVisible();
  expect(await page.evaluate(() => window.__wailsCalls.ApplyUpdate.length)).toBe(0);
  await expect(dialog.getByRole('link')).toHaveAttribute('href', available.downloadUrls['xdev-windows-amd64-v1.0.0-beta.2.zip']);
  await accessibleDialog(page);
  await dialog.getByRole('button', { name: 'Download and install', exact: true }).click();
  await expect.poll(() => page.evaluate(() => window.__wailsCalls.ApplyUpdate.length)).toBe(1);
  await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeDisabled();
  await expect(dialog).toHaveAttribute('aria-busy', 'true');
  await emitProgress(page);
  await expect(dialog.getByRole('progressbar')).toHaveAttribute('value', '32');
  await expect(dialog.locator('[data-testid="self-update-bytes"]')).toHaveText('320 B / 1000 B · 32%');
  await page.keyboard.press('Escape'); await page.locator('.self-update-mask').click({ position: { x: 10, y: 10 } });
  await expect(dialog).toBeVisible();
  await emitProgress(page, { phase: 'verifying', downloadedBytes: 1000, percent: 99 });
  await expect(dialog).toContainText('Verifying checksum');
  await emitProgress(page, { phase: 'extracting', downloadedBytes: 1000, percent: 99 });
  await expect(dialog).toContainText('Extracting');
  await emitProgress(page, { phase: 'installing', downloadedBytes: 1000, percent: 99 });
  await expect(dialog).toContainText('Installing');
  await emitProgress(page, { phase: 'completed', downloadedBytes: 1000, percent: 100 });
  await expect(dialog.locator('[data-testid="self-update-restart"]')).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeDisabled();
  await page.evaluate(() => window.__wailsFinishSelfUpdate());
  await expect(dialog.locator('[data-testid="self-update-restart"]')).toContainText('Close and reopen XDev');
  await expect(dialog).toContainText('Updated to v1.0.0-beta.2');
  await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeEnabled();
  await expect(dialog).toHaveAttribute('aria-busy', 'false');
  expect(await page.evaluate(() => window.__wailsCalls.WindowClose)).toBe(0);
  await accessibleDialog(page);
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(trigger).toBeFocused();
});

test('settings check error is inline, retry clears it and download action opens the same confirmation', async ({ page }, testInfo) => {
  await openApp(page, testInfo, { checks: [{ ...available, hasUpdate: false, error: 'network offline' }, available], applies: [installed] });
  const trigger = await checkFrom(page, 'settings');
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('alert')).toContainText('network offline');
  await expect(page.locator('[data-testid="settings-self-update"] [role="alert"]')).toContainText('network offline');
  await expect(page.locator('[data-testid="settings-self-update"]')).not.toContainText('Up to date');
  await accessibleDialog(page);
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(trigger).toBeFocused();
  await page.locator('[data-action="retry-self-update"]').click();
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('alert')).toHaveCount(0);
  await expect.poll(() => page.evaluate(() => window.__wailsCalls.CheckForUpdate.length)).toBe(2);
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(page.locator('[data-testid="settings-self-update"] [role="alert"]')).toHaveCount(0);
  const download = page.locator('[data-action="open-self-update"]');
  await expect(download).toHaveText('Download and install');
  await download.click(); await expect(dialog).toBeVisible();
  expect(await page.evaluate(() => window.__wailsCalls.ApplyUpdate.length)).toBe(0);
  await dialog.getByRole('button', { name: 'Download and install', exact: true }).click();
  await expect(dialog.locator('[data-testid="self-update-restart"]')).toBeVisible();
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(page.locator('[data-testid="settings-self-update"]')).toContainText('Close and reopen XDev');
  await expect(page.locator('[data-action="open-self-update"]')).toHaveCount(0);
});

test('download/install failure keeps concrete error and retries the existing update API', async ({ page }, testInfo) => {
  await openApp(page, testInfo, { checks: [available], applies: [{ ...available, error: 'checksum mismatch' }, installed] });
  await checkFrom(page);
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Download and install', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('checksum mismatch');
  await emitProgress(page); // Delayed progress must not turn the returned error back into busy.
  await expect(dialog.getByRole('alert')).toContainText('checksum mismatch');
  await expect(dialog.locator('[data-testid="self-update-restart"]')).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeEnabled();
  await accessibleDialog(page);
  await dialog.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(dialog.getByRole('alert')).toHaveCount(0);
  await expect(dialog.locator('[data-testid="self-update-restart"]')).toBeVisible();
  expect(await page.evaluate(() => window.__wailsCalls.ApplyUpdate.length)).toBe(2);
});

test('refetched no-update is not claimed as installed successfully', async ({ page }, testInfo) => {
  await openApp(page, testInfo, { checks: [available], applies: [{ current: available.latest, latest: available.latest, hasUpdate: false, applied: false }] });
  await checkFrom(page);
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Download and install', exact: true }).click();
  await expect(dialog).toContainText('Already up to date. No update was installed.');
  await emitProgress(page);
  await expect(dialog).toContainText('Already up to date. No update was installed.');
  await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeEnabled();
  await expect(dialog.locator('[data-testid="self-update-restart"]')).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Download and install', exact: true })).toHaveCount(0);
  await expect(page.locator('.n-message-container')).not.toContainText('Updated to');
  expect(await page.evaluate(() => window.__wailsCalls.WindowClose)).toBe(0);
});

test('an up-to-date manual check never opens an installation dialog', async ({ page }, testInfo) => {
  await openApp(page, testInfo);
  await checkFrom(page, 'settings');
  await expect(page.locator('[data-testid="settings-self-update"]')).toContainText('Up to date');
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(await page.evaluate(() => window.__wailsCalls.ApplyUpdate.length)).toBe(0);
});

test('confirmation traps keyboard focus, Escape closes only idle, and restores the check trigger', async ({ page }, testInfo) => {
  await openApp(page, testInfo, { checks: [available, available], applies: [installed], holdApply: true });
  const trigger = await checkFrom(page);
  const dialog = page.getByRole('dialog');
  const close = dialog.getByRole('button', { name: 'Close', exact: true });
  const update = dialog.getByRole('button', { name: 'Download and install', exact: true });
  await expect(close).toBeFocused();
  await page.keyboard.press('Control+K');
  await expect(page.getByRole('dialog')).toHaveCount(1);
  await expect(close).toBeFocused();
  await page.keyboard.press('Shift+Tab'); await expect(update).toBeFocused();
  await page.keyboard.press('Tab'); await expect(close).toBeFocused();
  await trigger.evaluate(element => element.focus()); await expect(close).toBeFocused();
  await page.keyboard.press('Escape'); await expect(dialog).toHaveCount(0); await expect(trigger).toBeFocused();
  await trigger.click(); await update.click();
  await expect(dialog).toBeFocused();
  await page.keyboard.press('Tab'); await expect(dialog).toBeFocused();
  await page.keyboard.press('Escape'); await expect(dialog).toBeVisible();
  await page.evaluate(() => window.__wailsFinishSelfUpdate());
  await expect(close).toBeFocused();
  await page.keyboard.press('Escape'); await expect(dialog).toHaveCount(0); await expect(trigger).toBeFocused();
});

test('startup notify has download/install and ignore actions in the same accessible dialog', async ({ page }, testInfo) => {
  await openApp(page, testInfo, {}, 'zh');
  await page.evaluate(data => window.__wailsEmit('self-update', { action: 'notify', ...data }), available);
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('button', { name: '下载并安装', exact: true })).toBeVisible();
  await expect(dialog.getByRole('button', { name: '忽略并继续', exact: true })).toBeVisible();
  await accessibleDialog(page);
  await dialog.getByRole('button', { name: '忽略并继续', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(await page.evaluate(() => window.__wailsCalls.IgnoreSelfUpdate)).toEqual([[available.latest]]);
  await page.evaluate(data => window.__wailsEmit('self-update', { action: 'notify', ...data }), available);
  await expect(dialog).toHaveCount(0);
});

test('auto-update completed progress is still busy until its real terminal result; errors remain actionable', async ({ page }, testInfo) => {
  await openApp(page, testInfo, { applies: [installed] });
  await emitProgress(page, { source: 'startup' });
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('progressbar')).toHaveAttribute('value', '32');
  await emitProgress(page, { source: 'startup', phase: 'completed', downloadedBytes: 1000, percent: 100 });
  await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeDisabled();
  await expect(dialog.locator('[data-testid="self-update-restart"]')).toHaveCount(0);
  await page.evaluate(data => window.__wailsEmit('self-update', { action: 'applied-error', ...data, error: 'access denied' }), available);
  await expect(dialog.getByRole('alert')).toContainText('access denied');
  await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeEnabled();
  await dialog.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(dialog.locator('[data-testid="self-update-restart"]')).toBeVisible();
});
