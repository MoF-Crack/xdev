// Naive UI theme overrides for the Spec 040 theme suite.
//
// Palettes mirror the CSS blocks in src/styles/theme.css and the registry
// in src/theme/themes.js (guarded by frontend/test/theme.test.js).
// Dark-family themes build on darkTheme, light-family on lightTheme.
import { darkTheme, lightTheme } from 'naive-ui';
import { THEMES } from './themes.js';

const FONT_UI = 'Inter, "Segoe UI Variable Text", system-ui, -apple-system, "Segoe UI", "PingFang SC", "Microsoft YaHei UI", "Noto Sans SC", sans-serif';

// value → palette (kept keyed by theme name for direct lookup in tests)
const palettes = {
  dark: { accent: '#5c9cff', hover: '#7fb4ff', pressed: '#4791f2', app: '#0e1116', panel: '#14181f', card: '#191e27', elevated: '#1f2531', text: '#e8ecf3', text2: '#9aa5b8', muted: '#7f8a9e', dim: '#545e6e', border: 'rgba(140, 155, 180, 0.16)', soft: 'rgba(92, 156, 255, 0.13)', onAccent: '#0b1220', success: '#3fb950', warning: '#d9a445', error: '#f26d5f', info: '#5c9cff', shadow: '0 8px 24px rgba(0, 0, 0, 0.34)' },
  midnight: { accent: '#63c7e6', hover: '#86d5ef', pressed: '#4fb9d9', app: '#0a0e1a', panel: '#0e1526', card: '#121b31', elevated: '#17223c', text: '#e6edf7', text2: '#9aa8c7', muted: '#7e8cae', dim: '#535f7d', border: 'rgba(140, 160, 205, 0.16)', soft: 'rgba(99, 199, 230, 0.13)', onAccent: '#04222e', success: '#4cc38a', warning: '#e0b34c', error: '#ef6d79', info: '#63c7e6', shadow: '0 8px 24px rgba(2, 6, 16, 0.5)' },
  nord: { accent: '#88c0d0', hover: '#a3d2df', pressed: '#71a9bd', app: '#2e3440', panel: '#333b4a', card: '#3b4252', elevated: '#434c5e', text: '#eceff4', text2: '#d8dee9', muted: '#b9c3d2', dim: '#77839a', border: 'rgba(216, 222, 233, 0.14)', soft: 'rgba(136, 192, 208, 0.15)', onAccent: '#1f2530', success: '#a3be8c', warning: '#ebcb8b', error: '#bf616a', info: '#81a1c1', shadow: '0 8px 24px rgba(15, 20, 30, 0.4)' },
  ember: { accent: '#e6a552', hover: '#f0bc74', pressed: '#d18e3a', app: '#15110c', panel: '#1a1510', card: '#201a13', elevated: '#261f16', text: '#f0e9dd', text2: '#c0b4a1', muted: '#a4957f', dim: '#6f6455', border: 'rgba(220, 200, 170, 0.14)', soft: 'rgba(230, 165, 82, 0.14)', onAccent: '#1d1405', success: '#96b06a', warning: '#dec452', error: '#e0755c', info: '#c8a06b', shadow: '0 8px 24px rgba(8, 6, 3, 0.45)' },
  light: { accent: '#2f6fed', hover: '#1f5bd8', pressed: '#1747b6', app: '#f2f4f8', panel: '#f7f9fb', card: '#ffffff', elevated: '#eaedf3', text: '#1c2534', text2: '#4a5870', muted: '#5d6b84', dim: '#93a0b4', border: 'rgba(28, 37, 52, 0.1)', soft: 'rgba(47, 111, 237, 0.09)', onAccent: '#ffffff', success: '#2e9e5b', warning: '#b97f24', error: '#d64545', info: '#2f6fed', shadow: '0 10px 28px rgba(28, 37, 52, 0.1)' },
  'eye-care': { accent: '#4e7d4e', hover: '#447044', pressed: '#3a613a', app: '#f4f1e8', panel: '#faf8f0', card: '#fdfbf4', elevated: '#efeadc', text: '#3a3126', text2: '#655a4b', muted: '#857a68', dim: '#a99c89', border: 'rgba(58, 49, 38, 0.12)', soft: 'rgba(78, 125, 78, 0.12)', onAccent: '#ffffff', success: '#4e7d4e', warning: '#b8860b', error: '#b65438', info: '#52808c', shadow: '0 10px 26px rgba(61, 53, 40, 0.12)' },
};

function createOverrides(p) {
  return {
    common: {
      fontFamily: FONT_UI, fontSize: '14px',
      primaryColor: p.accent, primaryColorHover: p.hover, primaryColorPressed: p.pressed, primaryColorSuppl: p.accent,
      infoColor: p.info, successColor: p.success, warningColor: p.warning, errorColor: p.error,
      textColorBase: p.text, textColor1: p.text, textColor2: p.text2, textColor3: p.muted, placeholderColor: p.dim,
      dividerColor: p.border, borderColor: p.border,
      inputColor: p.panel, actionColor: p.elevated, tableColor: p.card, hoverColor: p.soft,
      bodyColor: p.app, cardColor: p.card, modalColor: p.panel, popoverColor: p.panel, tooltipColor: p.card,
      borderRadius: '6px', borderRadiusSmall: '4px',
      boxShadow1: p.shadow, boxShadow2: p.shadow,
    },
    Empty: { textColor: p.muted, extraTextColor: p.text2 },
    Button: { textColorPrimary: p.onAccent, colorPrimary: p.accent, colorHoverPrimary: p.hover, colorPressedPrimary: p.pressed, colorFocusPrimary: p.pressed, borderPrimary: `1px solid ${p.accent}`, borderHoverPrimary: `1px solid ${p.accent}`, borderPressedPrimary: `1px solid ${p.accent}`, borderFocusPrimary: `1px solid ${p.accent}` },
    Menu: { itemTextColor: p.muted, itemTextColorHover: p.accent, itemTextColorActive: p.accent, itemIconColor: p.muted, itemIconColorHover: p.accent, itemIconColorActive: p.accent, itemColorActive: p.soft, itemColorHover: p.soft },
    Card: { color: p.card, borderColor: p.border, titleTextColor: p.text },
    Input: { color: p.panel, colorFocus: p.panel, borderColor: p.border, borderColorHover: p.accent, borderColorFocus: p.accent, textColor: p.text, placeholderColor: p.dim, boxShadowFocus: `0 0 0 2px ${p.soft}` },
    DataTable: { thColor: p.panel, tdColor: p.card, tdColorHover: p.soft, borderColor: p.border, thTextColor: p.muted, tdTextColor: p.text },
    Tabs: { tabTextColorLine: p.muted, tabTextColorActiveLine: p.accent, tabTextColorHoverLine: p.accent, barColor: p.accent },
    Switch: { railColorActive: p.accent },
    Checkbox: { colorChecked: p.accent, borderChecked: `1px solid ${p.accent}`, checkMarkColor: p.onAccent },
    Radio: { dotColorActive: p.accent },
    LayoutSider: { color: p.panel, borderColor: p.border },
    Layout: { color: p.app, siderColor: p.panel },
    Progress: { fillColor: p.accent, textColorLineInner: p.onAccent },
  };
}

const configs = Object.fromEntries(Object.entries(palettes).map(([name, palette]) => [
  name,
  {
    // dark-family themes extend darkTheme; light-family (daylight, sepia) extend lightTheme
    theme: (THEMES.find((entry) => entry.value === name)?.family ?? 'dark') === 'light' ? lightTheme : darkTheme,
    overrides: createOverrides(palette),
  },
]));

export function getNaiveThemeConfig(themeName) { return configs[themeName] ?? configs.dark; }
export function getNaiveThemeOverrides(themeName) { return getNaiveThemeConfig(themeName).overrides; }
