// Naive UI mirrors the approved CSS palettes, including interaction states.
// Set every filled-primary ink state explicitly: Naive's defaults otherwise
// turn dark-theme hover/focus text white on a light accent fill.
import { darkTheme, lightTheme } from 'naive-ui';
import { DEFAULT_THEME, THEMES, getThemeMeta } from './themes.js';

const FONT_UI = 'Inter, "Segoe UI Variable Text", system-ui, -apple-system, "Segoe UI", "PingFang SC", "Microsoft YaHei UI", "Noto Sans SC", sans-serif';

const palettes = {
  dark: { accent: '#a4adff', hover: '#b7beff', pressed: '#929ce8', app: '#101522', panel: '#151c2b', card: '#1b2435', elevated: '#222d42', mutedBg: '#222d42', hoverBg: '#222d42', text: '#e6edfb', text2: '#acb9d0', muted: '#96a6c0', dim: '#62718d', border: '#303d53', strongBorder: '#62718d', soft: '#2b3557', onAccent: '#1b2040', success: '#84cfb3', successSoft: '#223e3b', warning: '#e7bd78', warningSoft: '#3c332a', error: '#ff96a6', info: '#83d2d1', shadow: '0 3px 14px #03081628', focus: '0 0 0 3px rgba(164, 173, 255, 0.4)', overlay: 'rgba(3, 8, 22, 0.6)' },
  midnight: { accent: '#63c7e6', hover: '#86d5ef', pressed: '#4fb9d9', app: '#0a0e1a', panel: '#0e1526', card: '#121b31', elevated: '#17223c', mutedBg: '#1a2745', hoverBg: 'rgba(150, 180, 230, 0.07)', text: '#e6edf7', text2: '#9aa8c7', muted: '#7e8cae', dim: '#535f7d', border: 'rgba(140, 160, 205, 0.16)', strongBorder: 'rgba(140, 160, 205, 0.32)', soft: 'rgba(99, 199, 230, 0.13)', onAccent: '#04222e', success: '#4cc38a', successSoft: 'rgba(76, 195, 138, 0.13)', warning: '#e0b34c', warningSoft: 'rgba(224, 179, 76, 0.13)', error: '#ef6d79', info: '#63c7e6', shadow: '0 8px 24px rgba(2, 6, 16, 0.5)', focus: '0 0 0 3px rgba(99, 199, 230, 0.35)', overlay: 'rgba(2, 5, 12, 0.62)' },
  nord: { accent: '#88c0d0', hover: '#a3d2df', pressed: '#71a9bd', app: '#2e3440', panel: '#333b4a', card: '#3b4252', elevated: '#434c5e', mutedBg: '#47505f', hoverBg: 'rgba(236, 239, 244, 0.06)', text: '#eceff4', text2: '#d8dee9', muted: '#b9c3d2', dim: '#77839a', border: 'rgba(216, 222, 233, 0.14)', strongBorder: 'rgba(216, 222, 233, 0.28)', soft: 'rgba(136, 192, 208, 0.15)', onAccent: '#1f2530', success: '#a3be8c', successSoft: 'rgba(163, 190, 140, 0.16)', warning: '#ebcb8b', warningSoft: 'rgba(235, 203, 139, 0.15)', error: '#bf616a', info: '#81a1c1', shadow: '0 8px 24px rgba(15, 20, 30, 0.4)', focus: '0 0 0 3px rgba(136, 192, 208, 0.4)', overlay: 'rgba(24, 30, 42, 0.55)' },
  ember: { accent: '#e6a552', hover: '#f0bc74', pressed: '#d18e3a', app: '#15110c', panel: '#1a1510', card: '#201a13', elevated: '#261f16', mutedBg: '#2a2218', hoverBg: 'rgba(235, 215, 180, 0.06)', text: '#f0e9dd', text2: '#c0b4a1', muted: '#a4957f', dim: '#6f6455', border: 'rgba(220, 200, 170, 0.14)', strongBorder: 'rgba(220, 200, 170, 0.28)', soft: 'rgba(230, 165, 82, 0.14)', onAccent: '#1d1405', success: '#96b06a', successSoft: 'rgba(150, 176, 106, 0.14)', warning: '#dec452', warningSoft: 'rgba(222, 196, 82, 0.13)', error: '#e0755c', info: '#c8a06b', shadow: '0 8px 24px rgba(8, 6, 3, 0.45)', focus: '0 0 0 3px rgba(230, 165, 82, 0.35)', overlay: 'rgba(12, 8, 3, 0.6)' },
  light: { accent: '#5655cd', hover: '#4444b5', pressed: '#38389d', app: '#eef2fa', panel: '#f8f9fe', card: '#ffffff', elevated: '#f4f6fd', mutedBg: '#f4f6fd', hoverBg: '#f4f6fd', text: '#252c44', text2: '#576780', muted: '#64718a', dim: '#b4bfd6', border: '#dde3f0', strongBorder: '#b4bfd6', soft: '#ecebfc', onAccent: '#ffffff', success: '#21715a', successSoft: '#e5f2ed', warning: '#90601e', warningSoft: '#faf0dc', error: '#bd3d52', info: '#127d8b', shadow: '0 3px 12px #34447808', focus: '0 0 0 3px rgba(86, 85, 205, 0.25)', overlay: 'rgba(37, 44, 68, 0.4)' },
  'eye-care': { accent: '#326b58', hover: '#285b49', pressed: '#214c3d', app: '#e6ede7', panel: '#dce6df', card: '#f3f6ef', elevated: '#eaf0e8', mutedBg: '#eaf0e8', hoverBg: '#eaf0e8', text: '#223b33', text2: '#52675d', muted: '#5e7167', dim: '#a8baae', border: '#cad7ce', strongBorder: '#a8baae', soft: '#d8e7dc', onAccent: '#ffffff', success: '#28624e', successSoft: '#dceae0', warning: '#82531f', warningSoft: '#f0e4d0', error: '#ab444d', info: '#756583', shadow: '0 3px 12px #263d3010', focus: '0 0 0 3px rgba(50, 107, 88, 0.25)', overlay: 'rgba(34, 59, 51, 0.38)' },
};

function createOverrides(p) {
  const field = {
    color: p.elevated, textColor: p.text, placeholderColor: p.muted,
    border: `1px solid ${p.border}`, borderHover: `1px solid ${p.accent}`,
    borderFocus: `1px solid ${p.accent}`, boxShadowFocus: p.focus,
  };
  const primaryButton = {
    colorPrimary: p.accent, colorHoverPrimary: p.hover,
    colorPressedPrimary: p.pressed, colorFocusPrimary: p.hover, colorDisabledPrimary: p.accent,
    textColorPrimary: p.onAccent, textColorHoverPrimary: p.onAccent,
    textColorPressedPrimary: p.onAccent, textColorFocusPrimary: p.onAccent, textColorDisabledPrimary: p.onAccent,
    borderPrimary: `1px solid ${p.accent}`, borderHoverPrimary: `1px solid ${p.hover}`,
    borderPressedPrimary: `1px solid ${p.pressed}`, borderFocusPrimary: `1px solid ${p.hover}`,
    borderDisabledPrimary: `1px solid ${p.accent}`,
  };
  // Override every semantic state too, rather than inheriting Naive's green,
  // blue, amber and red hover colors from the unrelated base theme.
  const statusColors = Object.fromEntries(['info', 'success', 'warning', 'error'].flatMap((kind) => [
    [`${kind}Color`, p[kind]], [`${kind}ColorHover`, p[kind]],
    [`${kind}ColorPressed`, p[kind]], [`${kind}ColorSuppl`, p[kind]],
  ]));
  return {
    common: {
      fontFamily: FONT_UI, fontSize: '14px', baseColor: p.card,
      primaryColor: p.accent, primaryColorHover: p.hover, primaryColorPressed: p.pressed, primaryColorSuppl: p.accent,
      ...statusColors,
      textColorBase: p.text, textColor1: p.text, textColor2: p.text2, textColor3: p.muted,
      textColorDisabled: p.dim, placeholderColor: p.muted, placeholderColorDisabled: p.dim,
      iconColor: p.muted, iconColorHover: p.text2, iconColorPressed: p.text, iconColorDisabled: p.dim,
      dividerColor: p.border, borderColor: p.border,
      inputColor: p.elevated, inputColorDisabled: p.mutedBg, actionColor: p.elevated,
      bodyColor: p.app, cardColor: p.card, modalColor: p.card, popoverColor: p.card,
      tableColor: p.card, tableHeaderColor: p.elevated, tableColorHover: p.hoverBg, tableColorStriped: p.mutedBg,
      hoverColor: p.hoverBg, pressedColor: p.soft, codeColor: p.mutedBg, tagColor: p.mutedBg, tabColor: p.elevated,
      buttonColor2: p.elevated, buttonColor2Hover: p.hoverBg, buttonColor2Pressed: p.soft,
      railColor: p.strongBorder, progressRailColor: p.mutedBg,
      closeIconColor: p.muted, closeIconColorHover: p.text, closeIconColorPressed: p.text,
      closeColorHover: p.hoverBg, closeColorPressed: p.soft,
      clearColor: p.muted, clearColorHover: p.text2, clearColorPressed: p.text,
      scrollbarColor: p.strongBorder, scrollbarColorHover: p.muted,
      borderRadius: '6px', borderRadiusSmall: '4px',
      boxShadow1: p.shadow, boxShadow2: p.shadow, boxShadow3: p.shadow,
    },
    Empty: { textColor: p.muted, extraTextColor: p.text2 },
    Button: primaryButton,
    Menu: {
      itemTextColor: p.text2, itemTextColorHover: p.text, itemTextColorActive: p.accent, itemTextColorActiveHover: p.accent,
      itemIconColor: p.text2, itemIconColorHover: p.text, itemIconColorActive: p.accent, itemIconColorActiveHover: p.accent,
      itemColorActive: p.soft, itemColorActiveHover: p.soft, itemColorActiveCollapsed: p.soft, itemColorHover: p.hoverBg,
    },
    Card: { color: p.card, borderColor: p.border, titleTextColor: p.text },
    Input: { ...field, colorFocus: p.elevated },
    // Select uses peer themes, not Input's theme; override the real peer keys.
    Select: { peers: { InternalSelection: { ...field, colorActive: p.elevated, borderActive: `1px solid ${p.accent}`, boxShadowActive: p.focus } } },
    DataTable: { thColor: p.elevated, tdColor: p.card, tdColorHover: p.hoverBg, borderColor: p.border, thTextColor: p.text2, tdTextColor: p.text },
    Tabs: { tabTextColorLine: p.text2, tabTextColorActiveLine: p.accent, tabTextColorHoverLine: p.accent, barColor: p.accent, tabColorSegment: p.card },
    Switch: { railColor: p.strongBorder, railColorActive: p.accent, buttonColor: p.onAccent, textColor: p.onAccent, boxShadowFocus: p.focus },
    Checkbox: { color: p.card, colorChecked: p.accent, borderChecked: `1px solid ${p.accent}`, checkMarkColor: p.onAccent, boxShadowFocus: p.focus },
    Radio: { color: p.card, colorActive: p.card, dotColorActive: p.accent, boxShadowFocus: p.focus },
    Layout: { color: p.app, siderColor: p.panel, siderBorderColor: p.border, headerColor: p.panel, footerColor: p.panel },
    Progress: { fillColor: p.accent, textColorLineInner: p.onAccent },
    // Tooltip derives its own dark background instead of reading the common
    // surface token, so opt it into the same surface/text system explicitly.
    Tooltip: { color: p.card, textColor: p.text2 },
    Alert: { colorSuccess: p.successSoft, colorWarning: p.warningSoft },
    Tag: { colorSuccess: p.successSoft, colorBorderedSuccess: p.successSoft, colorWarning: p.warningSoft, colorBorderedWarning: p.warningSoft, textColorChecked: p.onAccent },
  };
}

const configs = Object.fromEntries(THEMES.map(({ value, family }) => [
  value,
  {
    theme: family === 'light' ? lightTheme : darkTheme,
    overrides: createOverrides(palettes[value]),
  },
]));

export function getNaiveThemeConfig(themeName = DEFAULT_THEME) { return configs[getThemeMeta(themeName).value]; }
export function getNaiveThemeOverrides(themeName) { return getNaiveThemeConfig(themeName).overrides; }
