<template>
  <FormSection id="appearance" :title="t('settings.theme')">
    <n-radio-group :value="theme" name="theme" aria-label="Theme" @update:value="$emit('update:theme', $event)">
      <div class="theme-grid">
        <n-radio
          v-for="option in options" :key="option.value" :value="option.value"
          class="theme-card" :class="{ 'theme-card--selected': option.value === theme }"
          :data-action="option.value === theme ? 'theme' : `theme-${option.value}`"
        >
          <span class="theme-card__inner">
            <span class="theme-card__preview" :style="{ background: option.swatch.bg }">
              <span class="theme-card__preview-panel" :style="{ background: option.swatch.panel }" />
              <span class="theme-card__preview-accent" :style="{ background: option.swatch.accent }"><span :style="{ background: option.swatch.onAccent }" /></span>
            </span>
            <span class="theme-card__labels">
              <span class="theme-card__name">{{ t(option.labelKey) }}</span>
              <span class="theme-card__family">{{ t(option.family === 'light' ? 'theme.family_light' : 'theme.family_dark') }}</span>
            </span>
            <span class="theme-card__check" aria-hidden="true">✓</span>
          </span>
        </n-radio>
      </div>
    </n-radio-group>
  </FormSection>
</template>
<script setup>
import { NRadio, NRadioGroup } from 'naive-ui';
import FormSection from '../FormSection.vue';
import { THEMES } from '../../theme/themes.js';
defineProps({ theme: { type: String, default: 'dark' }, t: { type: Function, required: true } });
defineEmits(['update:theme']);
const options = THEMES;
</script>
<style scoped>
.theme-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: var(--xdev-space-3); }
.theme-card { margin: 0; display: block; width: 100%; }
.theme-card :deep(.n-radio__dot-wrapper) { display: none; }
.theme-card :deep(.n-radio__label) { display: block; width: 100%; padding: 0; }
.theme-card__inner { position: relative; display: grid; gap: var(--xdev-space-2); width: 100%; padding: var(--xdev-space-2); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-md); background: var(--xdev-bg-card); transition: border-color var(--xdev-motion-fast) var(--xdev-ease-standard); }
.theme-card:hover .theme-card__inner { border-color: var(--xdev-border-strong); }
.theme-card--selected .theme-card__inner { border-color: var(--xdev-accent); box-shadow: 0 0 0 1px var(--xdev-accent); }
.theme-card__check { position: absolute; top: var(--xdev-space-2); right: var(--xdev-space-3); color: var(--xdev-accent); font-size: 13px; font-weight: 700; opacity: 0; }
.theme-card--selected .theme-card__check { opacity: 1; }
.theme-card__preview { display: flex; align-items: center; gap: var(--xdev-space-2); height: 52px; padding: var(--xdev-space-2); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-sm); overflow: hidden; }
.theme-card__preview-panel { width: 46%; height: 100%; border: 1px solid var(--xdev-border-subtle); border-radius: 2px; }
.theme-card__preview-accent { margin-left: auto; display: inline-flex; align-items: center; justify-content: center; width: 16px; height: 16px; border-radius: 50%; flex: 0 0 16px; }
.theme-card__preview-accent span { width: 6px; height: 6px; border-radius: 50%; }
.theme-card__labels { display: flex; align-items: baseline; justify-content: space-between; gap: var(--xdev-space-2); }
.theme-card__name { color: var(--xdev-text-primary); font-size: var(--xdev-font-size-sm); font-weight: 600; }
.theme-card__family { color: var(--xdev-text-muted); font-size: 11px; font-weight: 600; letter-spacing: 0.08em; text-transform: uppercase; white-space: nowrap; }
@media (max-width: 560px) { .theme-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
</style>
