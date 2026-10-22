// Spec 040: single source of truth for the theme suite.
//
// App.vue, AppContent.vue and ThemeSettings.vue all read this registry
// instead of hard-coding theme name lists. The swatch colors drive the
// live preview chips in the settings theme picker and are guarded against
// theme.css by theme.test.js (registry ↔ CSS ↔ naive-theme consistency).

export const THEMES = [
  {
    value: 'dark',
    labelKey: 'theme.dark',
    family: 'dark',
    swatch: { bg: '#0e1116', panel: '#14181f', accent: '#5c9cff', onAccent: '#0b1220' },
  },
  {
    value: 'midnight',
    labelKey: 'theme.midnight',
    family: 'dark',
    swatch: { bg: '#0a0e1a', panel: '#0e1526', accent: '#63c7e6', onAccent: '#04222e' },
  },
  {
    value: 'nord',
    labelKey: 'theme.nord',
    family: 'dark',
    swatch: { bg: '#2e3440', panel: '#333b4a', accent: '#88c0d0', onAccent: '#1f2530' },
  },
  {
    value: 'ember',
    labelKey: 'theme.ember',
    family: 'dark',
    swatch: { bg: '#15110c', panel: '#1a1510', accent: '#e6a552', onAccent: '#1d1405' },
  },
  {
    value: 'light',
    labelKey: 'theme.light',
    family: 'light',
    swatch: { bg: '#f2f4f8', panel: '#f7f9fb', accent: '#2f6fed', onAccent: '#ffffff' },
  },
  {
    value: 'eye-care',
    labelKey: 'theme.eye_care',
    family: 'light',
    swatch: { bg: '#f4f1e8', panel: '#faf8f0', accent: '#4e7d4e', onAccent: '#ffffff' },
  },
];

export const DEFAULT_THEME = 'dark';

export const THEME_VALUES = THEMES.map((theme) => theme.value);

export function isKnownTheme(name) {
  return THEME_VALUES.includes(name);
}

export function getThemeMeta(name) {
  return THEMES.find((theme) => theme.value === name) ?? THEMES.find((theme) => theme.value === DEFAULT_THEME);
}
