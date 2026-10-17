import { readFileSync, existsSync, readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const read = (path) => readFileSync(join(root, path), 'utf8');
const failures = [];

function expect(condition, message) {
  if (!condition) failures.push(message);
}

const app = read('src/App.vue');
const appContent = read('src/AppContent.vue');
const appShell = read('src/layouts/AppShell.vue');
const toolCatalogPage = read('src/pages/ToolCatalogPage.vue');
const navigation = read('src/navigation/navigation.js');
const setupWizard = read('src/SetupWizard.vue');
const commandPalette = read('src/components/CommandPalette.vue');
const toolDetailDrawer = read('src/components/ToolDetailDrawer.vue');
const theme = read('src/styles/theme.css');
// R8-04 附带修复：窗口最大化逻辑在 dc6edf3 抽到了这个 composable（AppContent 只
// 消费它）。原断言仍按 AppContent 的旧位置做字符串匹配 → 永久误报。这里改为对
// **逻辑实际所在文件**断言（强度不变：仍然要求具体调用与 `.catch(` 出现）。
const windowChrome = read('src/composables/useWindowChrome.js');
function sourceFilesIn(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) return sourceFilesIn(path);
    return /\.(?:vue|css|js|ts)$/.test(entry.name) ? [readFileSync(path, 'utf8')] : [];
  });
}
const sourceFiles = sourceFilesIn(join(root, 'src'));

expect(existsSync(join(root, 'src/styles/theme.css')), 'src/styles/theme.css should exist');
expect(app.includes("import './styles/theme.css'"), 'App.vue should import theme.css');
expect(existsSync(join(root, 'src/components/CommandPalette.vue')), 'CommandPalette component should exist');
expect(appContent.includes('<CommandPalette'), 'AppContent.vue should render CommandPalette');
expect(appContent.includes('<AppShell'), 'AppContent.vue should compose AppShell');
expect(!/v-(?:else-)?if\s*=\s*["']false["']/.test(appContent), 'AppContent should not retain disabled false template branches');
const fallbackIndex = appContent.indexOf('<ToolCatalogPage v-else');
expect(fallbackIndex > appContent.indexOf('<EnvironmentVariablesPage v-else-if='), 'ToolCatalog fallback should follow the extracted pages');
expect(toolCatalogPage.includes('v-model') || toolCatalogPage.includes('update:searchQuery'), 'tool catalog should own its search model');
expect(!appContent.includes('data-testid="tool-search"'), 'AppContent should not duplicate tool catalog search markup');
expect(navigation.includes("key: 'tools-group'"), 'registry should include a tools group');
expect(navigation.includes("key: 'environment-group'"), 'registry should include an environment group');
expect(navigation.includes("key: 'development-group'"), 'registry should include a development group');
expect(navigation.includes("key: 'settings-group'"), 'registry should include a settings group');
const knownNavKeys = [...navigation.matchAll(/key:\s*'([^']+)'/g)].map(m => m[1]);
expect(!knownNavKeys.some(k => k === 'custom-tools' || k === 'plugins'), 'navigation registry should not contain custom-tools or plugins');
expect(appShell.includes("window.addEventListener('keydown'"), 'AppShell should register global keyboard shortcuts');
expect(appContent.includes('useWindowChrome()'), 'AppContent should wire window chrome through the composable');
expect(windowChrome.includes('await WindowIsMaximised()'), 'maximize should immediately refresh Wails window state');
expect(windowChrome.includes('.catch('), 'Wails maximize polling should handle rejected state checks');
expect(!setupWizard.includes('#7D56F4'), 'SetupWizard should not use the old purple accent');
expect(!setupWizard.includes('#252540'), 'SetupWizard should not use the old purple card background');
expect(!setupWizard.includes('#1a1a2e'), 'SetupWizard should not use the old purple page background');
expect(!appContent.includes('<style'), 'AppContent should not retain page-specific styles');
for (const marker of ['languageOptions', 'pathModeOptions', 'local-path', 'version-row', 'fav-tool-card', 'sider-header', 'title-btn']) {
  expect(!appContent.includes(marker), `AppContent should not retain dead ${marker} code`);
}
// R8-04 附带修复：`switchToXdev` 从上面的死代码标记表移出。它写于 a6dae13（当时
// 确实没有调用方），但 1418bb3（v0.3.2「PM instance switch」）让 ToolDetailDrawer
// 开始 `emit('switch-xdev', ...)`，AppContent 也接上了 `@switch-xdev` → 该函数已是
// **活代码**，再断言「不得存在」就是反向误报。改为正向断言这条链路仍在（强度不低于
// 原来的否定断言：要求 emit 与 handler 两端都存在）。
expect(toolDetailDrawer.includes("emit('switch-xdev'"), 'ToolDetailDrawer should emit switch-xdev for managed PM instances');
expect(appContent.includes('@switch-xdev="switchToXdev"'), 'AppContent should handle switch-xdev from the drawer');
expect(appContent.includes('async function switchToXdev('), 'switchToXdev handler should exist (live code since 1418bb3)');
expect(!sourceFiles.some(file => /transition\s*:\s*all\b/i.test(file)), 'source should not use transition: all');
expect(!sourceFiles.some(file => /(?:#00e5ff|rgba\(0\s*,\s*229\s*,\s*255)/i.test(file)), 'source should not use the legacy cyan accent');

const definedVars = new Set([...theme.matchAll(/(--xdev-[\w-]+)\s*:/g)].map(match => match[1]));
const usedVars = new Set(sourceFiles.flatMap(file => [...file.matchAll(/var\((--xdev-[\w-]+)/g)].map(match => match[1])));
for (const variable of usedVars) expect(definedVars.has(variable), `${variable} should be defined in theme.css`);

const localeKeys = {};
for (const locale of ['en', 'zh']) {
  const messages = read(`src/i18n/locales/${locale}.ts`);
  const keys = [...messages.matchAll(/^\s*['"]([^'"]+)['"]\s*:/gm)].map(match => match[1]);
  expect(new Set(keys).size === keys.length, `${locale} locale should not contain duplicate keys`);
  localeKeys[locale] = new Set(keys);
}
const enOnly = [...localeKeys.en].filter(key => !localeKeys.zh.has(key));
const zhOnly = [...localeKeys.zh].filter(key => !localeKeys.en.has(key));
expect(!enOnly.length && !zhOnly.length, `en and zh locales should contain the same keys (en-only: ${enOnly.join(', ') || 'none'}; zh-only: ${zhOnly.join(', ') || 'none'})`);
expect(read('index.html').includes('@media (prefers-reduced-motion: reduce)'), 'pre-mount styles should honor reduced motion');

if (failures.length > 0) {
  console.error('Smoke test failed:');
  for (const failure of failures) console.error(`- ${failure}`);
  process.exit(1);
}

console.log('Smoke test passed');
