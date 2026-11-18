// Persisted theme values are a compatibility contract. Keep the three approved
// preview themes first and retain the older palettes behind the main choices.
// Swatches mirror theme.css and Naive UI; theme.test.js guards the full mapping.
export const THEMES = [
  {
    value: 'eye-care',
    labelKey: 'theme.eye_care',
    descriptionKey: 'theme.eye_care_description',
    family: 'light',
    group: 'main',
    swatch: { bg: '#e6ede7', panel: '#dce6df', card: '#f3f6ef', raised: '#eaf0e8', text: '#223b33', secondary: '#52675d', muted: '#5e7167', accent: '#326b58', soft: '#d8e7dc', onAccent: '#ffffff' },
  },
  {
    value: 'light',
    labelKey: 'theme.light',
    descriptionKey: 'theme.light_description',
    family: 'light',
    group: 'main',
    swatch: { bg: '#eef2fa', panel: '#f8f9fe', card: '#ffffff', raised: '#f4f6fd', text: '#252c44', secondary: '#576780', muted: '#64718a', accent: '#5655cd', soft: '#ecebfc', onAccent: '#ffffff' },
  },
  {
    value: 'dark',
    labelKey: 'theme.dark',
    descriptionKey: 'theme.dark_description',
    family: 'dark',
    group: 'main',
    swatch: { bg: '#101522', panel: '#151c2b', card: '#1b2435', raised: '#222d42', text: '#e6edfb', secondary: '#acb9d0', muted: '#96a6c0', accent: '#a4adff', soft: '#2b3557', onAccent: '#1b2040' },
  },
  {
    value: 'midnight',
    labelKey: 'theme.midnight',
    family: 'dark',
    group: 'legacy',
    swatch: { bg: '#0a0e1a', panel: '#0e1526', card: '#121b31', raised: '#17223c', text: '#e6edf7', secondary: '#9aa8c7', muted: '#7e8cae', accent: '#63c7e6', soft: 'rgba(99, 199, 230, 0.13)', onAccent: '#04222e' },
  },
  {
    value: 'nord',
    labelKey: 'theme.nord',
    family: 'dark',
    group: 'legacy',
    swatch: { bg: '#2e3440', panel: '#333b4a', card: '#3b4252', raised: '#434c5e', text: '#eceff4', secondary: '#d8dee9', muted: '#b9c3d2', accent: '#88c0d0', soft: 'rgba(136, 192, 208, 0.15)', onAccent: '#1f2530' },
  },
  {
    value: 'ember',
    labelKey: 'theme.ember',
    family: 'dark',
    group: 'legacy',
    swatch: { bg: '#15110c', panel: '#1a1510', card: '#201a13', raised: '#261f16', text: '#f0e9dd', secondary: '#c0b4a1', muted: '#a4957f', accent: '#e6a552', soft: 'rgba(230, 165, 82, 0.14)', onAccent: '#1d1405' },
  },
];

// Do not silently migrate an existing preference or change first-run behavior.
export const DEFAULT_THEME = 'dark';
export const MAIN_THEMES = THEMES.filter((theme) => theme.group === 'main');
export const LEGACY_THEMES = THEMES.filter((theme) => theme.group === 'legacy');
export const THEME_VALUES = THEMES.map((theme) => theme.value);

export function isKnownTheme(name) {
  return THEME_VALUES.includes(name);
}

export function getThemeMeta(name) {
  return THEMES.find((theme) => theme.value === name) ?? THEMES.find((theme) => theme.value === DEFAULT_THEME);
}
