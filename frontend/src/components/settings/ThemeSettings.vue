<template>
  <FormSection id="appearance" :title="t('settings.theme')">
    <n-radio-group :value="theme" name="theme" role="radiogroup" :aria-label="t('settings.theme')" @update:value="$emit('update:theme', $event)">
      <div v-for="group in groups" :key="group.value" class="theme-group" :data-theme-group="group.value">
        <p class="theme-group__title">{{ t(group.labelKey) }}</p>
        <div class="theme-grid" :class="{ 'theme-grid--legacy': group.value === 'legacy' }">
          <n-radio
            v-for="option in group.options" :key="option.value" :value="option.value"
            class="theme-card" :class="{ 'theme-card--selected': option.value === theme }"
            :data-action="option.value === theme ? 'theme' : `theme-${option.value}`"
          >
            <span class="theme-card__inner">
              <span class="theme-card__preview" :style="previewStyle(option.swatch)" aria-hidden="true">
                <span class="theme-card__preview-panel">
                  <span class="theme-card__preview-brand" />
                  <span class="theme-card__preview-nav" />
                  <span class="theme-card__preview-nav" />
                  <span class="theme-card__preview-nav" />
                </span>
                <span class="theme-card__preview-content">
                  <span class="theme-card__preview-title" />
                  <span class="theme-card__preview-surface">
                    <span class="theme-card__preview-line" />
                    <span class="theme-card__preview-line" />
                    <span class="theme-card__preview-accent"><span /></span>
                  </span>
                </span>
              </span>
              <span class="theme-card__labels">
                <span class="theme-card__name">{{ t(option.labelKey) }}</span>
                <span class="theme-card__family">{{ t(option.family === 'light' ? 'theme.family_light' : 'theme.family_dark') }}</span>
              </span>
              <span v-if="option.descriptionKey" class="theme-card__description">{{ t(option.descriptionKey) }}</span>
              <span class="theme-card__check" aria-hidden="true">✓</span>
            </span>
          </n-radio>
        </div>
      </div>
    </n-radio-group>
  </FormSection>
</template>
<script setup>
import { NRadio, NRadioGroup } from 'naive-ui';
import FormSection from '../FormSection.vue';
import { DEFAULT_THEME, MAIN_THEMES, LEGACY_THEMES } from '../../theme/themes.js';

defineProps({ theme: { type: String, default: DEFAULT_THEME }, t: { type: Function, required: true } });
defineEmits(['update:theme']);
const groups = [
  { value: 'main', labelKey: 'theme.main_themes', options: MAIN_THEMES },
  { value: 'legacy', labelKey: 'theme.legacy_themes', options: LEGACY_THEMES },
];
function previewStyle(swatch) {
  return Object.fromEntries(Object.entries(swatch).map(([key, value]) => [`--preview-${key}`, value]));
}
</script>
<style scoped>
.theme-group + .theme-group { margin-top: var(--xdev-space-5); padding-top: var(--xdev-space-4); border-top: 1px solid var(--xdev-border-subtle); }
.theme-group__title { margin: 0 0 var(--xdev-space-3); color: var(--xdev-text-secondary); font-size: var(--xdev-font-size-xs); font-weight: 600; }
.theme-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--xdev-space-3); }
.theme-card { margin: 0; display: block; width: 100%; min-width: 0; }
/* Keep the native radio focusable; display:none on this wrapper hides its input. */
.theme-card :deep(.n-radio__dot-wrapper) { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
.theme-card :deep(.n-radio__label) { display: block; width: 100%; padding: 0; }
.theme-card__inner { position: relative; display: grid; align-content: start; gap: var(--xdev-space-2); height: 100%; width: 100%; padding: var(--xdev-space-3); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-lg); background: var(--xdev-bg-card); transition: border-color var(--xdev-motion-fast) var(--xdev-ease-standard); }
.theme-card:hover .theme-card__inner { border-color: var(--xdev-border-strong); }
.theme-card--selected .theme-card__inner { border-color: var(--xdev-accent); box-shadow: 0 0 0 1px var(--xdev-accent); }
.theme-card:focus-within .theme-card__inner { outline: 2px solid var(--xdev-accent); outline-offset: 3px; box-shadow: var(--xdev-focus-ring); }
.theme-card__check { position: absolute; top: var(--xdev-space-2); right: var(--xdev-space-2); display: grid; place-items: center; width: 20px; height: 20px; border-radius: 50%; background: var(--xdev-accent); color: var(--xdev-text-on-accent); font-size: 12px; font-weight: 700; opacity: 0; }
.theme-card--selected .theme-card__check { opacity: 1; }
.theme-card__preview { display: flex; height: 112px; border-radius: var(--xdev-radius-sm); background: var(--preview-bg); overflow: hidden; }
.theme-card__preview-panel { display: flex; flex: 0 0 27%; flex-direction: column; gap: 8px; padding: 14px 8px; background: var(--preview-panel); }
.theme-card__preview-brand { width: 14px; height: 5px; margin-bottom: 5px; border-radius: 2px; background: var(--preview-accent); }
.theme-card__preview-nav { width: 100%; height: 4px; border-radius: 2px; background: var(--preview-muted); opacity: 0.45; }
.theme-card__preview-nav:nth-child(2) { background: var(--preview-accent); opacity: 1; }
.theme-card__preview-nav:last-child { width: 70%; }
.theme-card__preview-content { display: grid; align-content: start; gap: 10px; flex: 1; min-width: 0; padding: 14px 10px; }
.theme-card__preview-title { width: 46%; height: 5px; border-radius: 2px; background: var(--preview-text); }
.theme-card__preview-surface { display: grid; gap: 7px; padding: 10px; border-radius: 4px; background: var(--preview-card); }
.theme-card__preview-line { width: 83%; height: 4px; border-radius: 2px; background: var(--preview-secondary); opacity: 0.55; }
.theme-card__preview-line:nth-child(2) { width: 62%; }
.theme-card__preview-accent { display: grid; place-items: center; width: 38%; height: 14px; margin-top: 1px; border-radius: 3px; background: var(--preview-accent); }
.theme-card__preview-accent span { width: 55%; height: 3px; border-radius: 1px; background: var(--preview-onAccent); }
.theme-card__labels { display: flex; align-items: baseline; justify-content: space-between; gap: var(--xdev-space-2); }
.theme-card__name { color: var(--xdev-text-primary); font-size: var(--xdev-font-size-sm); font-weight: 600; }
.theme-card__family { color: var(--xdev-text-muted); font-size: 11px; white-space: nowrap; }
.theme-card__description { color: var(--xdev-text-secondary); font-size: var(--xdev-font-size-xs); line-height: 1.6; }
.theme-grid--legacy .theme-card__inner { padding: var(--xdev-space-2); border-radius: var(--xdev-radius-md); }
.theme-grid--legacy .theme-card__preview { height: 52px; }
.theme-grid--legacy .theme-card__preview-panel { padding: 9px 7px; gap: 5px; }
.theme-grid--legacy .theme-card__preview-brand { margin: 0; }
.theme-grid--legacy .theme-card__preview-nav:last-child { display: none; }
.theme-grid--legacy .theme-card__preview-content { padding: 9px 8px; gap: 6px; }
.theme-grid--legacy .theme-card__preview-surface { padding: 6px; gap: 4px; }
.theme-grid--legacy .theme-card__preview-line:nth-child(2) { display: none; }
.theme-grid--legacy .theme-card__preview-accent { height: 8px; }
@media (max-width: 560px) { .theme-grid { grid-template-columns: 1fr; } .theme-card__preview { height: 96px; } .theme-grid--legacy { grid-template-columns: repeat(auto-fit, minmax(130px, 1fr)); } }
</style>
