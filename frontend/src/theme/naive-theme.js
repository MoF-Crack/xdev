const palettes = {
  dark: {
    accent: '#818cf8', hover: '#a5b4fc', pressed: '#6366f1', app: '#111113', panel: '#18181b', card: '#202024', elevated: '#29292e',
    text: '#f4f4f5', text2: '#d4d4d8', muted: '#a1a1aa', dim: '#71717a', border: 'rgba(161, 161, 170, 0.16)', soft: 'rgba(129, 140, 248, 0.12)', onAccent: '#ffffff',
    success: '#34d399', warning: '#fbbf24', error: '#f87171', info: '#60a5fa', shadow: '0 8px 24px rgba(0, 0, 0, 0.28)',
  },
  light: {
    accent: '#4f46e5', hover: '#4338ca', pressed: '#3730a3', app: '#f7f7f8', panel: '#ffffff', card: '#ffffff', elevated: '#f4f4f5',
    text: '#18181b', text2: '#3f3f46', muted: '#52525b', dim: '#71717a', border: 'rgba(39, 39, 42, 0.1)', soft: 'rgba(79, 70, 229, 0.1)', onAccent: '#ffffff',
    success: '#047857', warning: '#b45309', error: '#dc2626', info: '#2563eb', shadow: '0 8px 24px rgba(24, 24, 27, 0.1)',
  },
};

function createOverrides(p) {
  return {
    common: { primaryColor: p.accent, primaryColorHover: p.hover, primaryColorPressed: p.pressed, primaryColorSuppl: p.accent, infoColor: p.info, successColor: p.success, warningColor: p.warning, errorColor: p.error, textColorBase: p.text, textColor1: p.text, textColor2: p.text2, textColor3: p.muted, placeholderColor: p.dim, dividerColor: p.border, borderColor: p.border, inputColor: p.panel, actionColor: p.elevated, tableColor: p.card, hoverColor: p.soft, bodyColor: p.app, cardColor: p.card, modalColor: p.panel, popoverColor: p.panel, tooltipColor: p.card, borderRadius: '8px', borderRadiusSmall: '4px', boxShadow1: p.shadow, boxShadow2: p.shadow },
    Button: { textColorPrimary: p.onAccent, colorPrimary: p.accent, colorHoverPrimary: p.hover, colorPressedPrimary: p.pressed, borderPrimary: `1px solid ${p.accent}` },
    Menu: { itemTextColor: p.muted, itemTextColorHover: p.accent, itemTextColorActive: p.accent, itemIconColor: p.muted, itemIconColorHover: p.accent, itemIconColorActive: p.accent, itemColorActive: p.soft, itemColorHover: p.soft },
    Card: { color: p.card, borderColor: p.border, titleTextColor: p.text },
    Input: { color: p.panel, colorFocus: p.panel, borderColor: p.border, borderColorHover: p.accent, borderColorFocus: p.accent, textColor: p.text, placeholderColor: p.dim, boxShadowFocus: `0 0 0 2px ${p.soft}` },
    DataTable: { thColor: p.panel, tdColor: p.card, tdColorHover: p.soft, borderColor: p.border, thTextColor: p.muted, tdTextColor: p.text },
    Tabs: { tabTextColorLine: p.muted, tabTextColorActiveLine: p.accent, tabTextColorHoverLine: p.accent, barColor: p.accent },
    Switch: { railColorActive: p.accent }, Checkbox: { colorChecked: p.accent, borderChecked: `1px solid ${p.accent}`, checkMarkColor: p.onAccent },
    LayoutSider: { color: p.panel, borderColor: p.border }, Layout: { color: p.app, siderColor: p.panel },
    Progress: { fillColor: p.accent, textColorLineInner: p.onAccent }, Badge: { color: p.accent }, Scrollbar: { color: p.border, colorHover: p.muted },
  };
}

const overrides = { dark: createOverrides(palettes.dark), light: createOverrides(palettes.light) };
export function getNaiveThemeOverrides(theme) { return overrides[theme] ?? overrides.dark; }
