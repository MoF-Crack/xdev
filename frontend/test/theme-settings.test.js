import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import { NButton, NConfigProvider, NInput, NSelect } from 'naive-ui';
import { h } from 'vue';
import ThemeSettings from '../src/components/settings/ThemeSettings.vue';
import { getNaiveThemeConfig } from '../src/theme/naive-theme.js';
import { MAIN_THEMES, LEGACY_THEMES, THEMES } from '../src/theme/themes.js';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const source = readFileSync(join(root, 'src/components/settings/ThemeSettings.vue'), 'utf8');
const labels = {
  'settings.theme': 'Theme', 'theme.main_themes': 'Main themes', 'theme.legacy_themes': 'Classic themes',
  'theme.dark': 'Dark', 'theme.light': 'Light', 'theme.eye_care': 'Eye care',
  'theme.midnight': 'Midnight', 'theme.nord': 'Nord', 'theme.ember': 'Ember',
  'theme.family_light': 'Light family', 'theme.family_dark': 'Dark family',
  'theme.eye_care_description': 'Muted green tones for longer sessions',
  'theme.light_description': 'Bright surfaces with clear contrast',
  'theme.dark_description': 'Soft periwinkle accents for low-light spaces',
};
const t = (key) => labels[key] ?? key;
const wrappers = [];
function render(theme = 'dark') {
  const wrapper = mount(ThemeSettings, { props: { theme, t }, attachTo: document.body });
  wrappers.push(wrapper);
  return wrapper;
}
afterEach(() => wrappers.splice(0).forEach((wrapper) => wrapper.unmount()));

describe('ThemeSettings', () => {
  it('shows three full main-theme cards followed by three selectable classic themes', () => {
    const wrapper = render();
    const groups = wrapper.findAll('[data-theme-group]');
    expect(groups.map((group) => group.attributes('data-theme-group'))).toEqual(['main', 'legacy']);
    expect(groups[0].findAll('input[type="radio"]').map((radio) => radio.element.value)).toEqual(MAIN_THEMES.map((theme) => theme.value));
    expect(groups[1].findAll('input[type="radio"]').map((radio) => radio.element.value)).toEqual(LEGACY_THEMES.map((theme) => theme.value));
    expect(wrapper.findAll('.theme-card__description')).toHaveLength(3);
    expect(wrapper.findAll('input[type="radio"]')).toHaveLength(6);
  });

  it.each(THEMES)('preserves $value radio value, checked selection and data-action contract', async ({ value }) => {
    const wrapper = render(value);
    const selected = wrapper.get('input:checked');
    expect(selected.element.value).toBe(value);
    expect(selected.attributes('name')).toBe('theme');
    expect(wrapper.get('[data-action="theme"] input').element).toBe(selected.element);
    expect(wrapper.findAll('.theme-card--selected')).toHaveLength(1);
    const next = value === 'light' ? 'midnight' : 'light';
    await wrapper.get(`[data-action="theme-${next}"] input`).setValue(true);
    expect(wrapper.emitted('update:theme')).toEqual([[next]]);
    await wrapper.setProps({ theme: next });
    expect(wrapper.get('[data-action="theme"] input').element.value).toBe(next);
    expect(wrapper.get(`input[value="${value}"]`).element.checked).toBe(false);
    expect(wrapper.findAll('[data-action="theme"]')).toHaveLength(1);
  });

  it('exposes localized native radio labels and hides only decorative preview content', () => {
    const wrapper = render();
    expect(wrapper.get('[role="radiogroup"]').attributes('aria-label')).toBe('Theme');
    for (const theme of THEMES) {
      const label = wrapper.get(`input[value="${theme.value}"]`).element.closest('label');
      expect(label.textContent).toContain(t(theme.labelKey));
      expect(label.querySelector('.theme-card__preview').getAttribute('aria-hidden')).toBe('true');
    }
    expect(wrapper.findAll('.theme-card__check[aria-hidden="true"]')).toHaveLength(6);
    // jsdom does not implement native radio arrow-key navigation. Verify that
    // the real inputs remain focusable and card focus reaches Naive's state.
    const radio = wrapper.get('input[value="eye-care"]');
    radio.element.focus();
    expect(document.activeElement).toBe(radio.element);
    expect(source).toContain('.theme-card:focus-within .theme-card__inner');
    expect(source).not.toMatch(/\.n-radio__dot-wrapper\)\s*\{[^}]*display:\s*none/);
  });

  it('paints each preview from its own palette instead of the currently selected theme', () => {
    const wrapper = render('eye-care');
    const cards = wrapper.findAll('.theme-card');
    cards.forEach((card, i) => {
      const preview = card.get('.theme-card__preview').element;
      expect(preview.style.getPropertyValue('--preview-bg')).toBe(THEMES[i].swatch.bg);
      expect(preview.style.getPropertyValue('--preview-card')).toBe(THEMES[i].swatch.card);
      expect(preview.style.getPropertyValue('--preview-accent')).toBe(THEMES[i].swatch.accent);
      expect(preview.style.getPropertyValue('--preview-onAccent')).toBe(THEMES[i].swatch.onAccent);
    });
  });

  it.each(MAIN_THEMES)('applies the $value overrides to actual Naive primary buttons and fields', ({ value, swatch }) => {
    const config = getNaiveThemeConfig(value);
    const wrapper = mount(NConfigProvider, {
      props: { theme: config.theme, themeOverrides: config.overrides },
      global: { stubs: { transition: false } },
      slots: { default: () => [
        h(NButton, { type: 'primary' }, { default: () => 'Apply' }),
        h(NInput, { value: '', placeholder: 'Path' }),
        h(NSelect, { value: null, options: [{ value: 'one', label: 'One' }] }),
      ] },
    });
    wrappers.push(wrapper);
    const button = wrapper.get('.n-button').element;
    for (const state of ['', '-hover', '-pressed', '-focus', '-disabled']) expect(button.style.getPropertyValue(`--n-text-color${state}`)).toBe(swatch.onAccent);
    const input = wrapper.get('.n-input').element;
    expect(input.style.getPropertyValue('--n-color')).toBe(swatch.raised);
    expect(input.style.getPropertyValue('--n-placeholder-color')).toBe(swatch.muted);
    const select = wrapper.get('.n-base-selection').element;
    expect(select.style.getPropertyValue('--n-color')).toBe(swatch.raised);
  });

  it.each(MAIN_THEMES)('mounts the $value theme picker under its actual Naive provider', ({ value }) => {
    const config = getNaiveThemeConfig(value);
    const wrapper = mount(NConfigProvider, {
      props: { theme: config.theme, themeOverrides: config.overrides },
      slots: { default: () => h(ThemeSettings, { theme: value, t }) },
    });
    wrappers.push(wrapper);
    expect(wrapper.findAll('input[type="radio"]')).toHaveLength(6);
    expect(wrapper.get('input:checked').element.value).toBe(value);
  });
});
