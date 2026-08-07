<template>
  <FormSection id="appearance" :title="t('settings.theme')">
    <n-radio-group :value="theme" name="theme" aria-label="Theme" @update:value="$emit('update:theme', $event)">
      <n-space class="theme-options" :size="8" wrap>
        <n-radio v-for="option in options" :key="option.value" :value="option.value" :data-action="option.value === theme ? 'theme' : `theme-${option.value}`">
          <span class="theme-option"><span class="theme-swatch" :class="`theme-swatch--${option.value}`" aria-hidden="true"><i /><b /></span>{{ t(option.label) }}</span>
        </n-radio>
      </n-space>
    </n-radio-group>
  </FormSection>
</template>
<script setup>
import { NRadio, NRadioGroup, NSpace } from 'naive-ui';
import FormSection from '../FormSection.vue';
defineProps({ theme: { type: String, default: 'dark' }, t: { type: Function, required: true } });
defineEmits(['update:theme']);
const options = [{ value: 'light', label: 'theme.light' }, { value: 'dark', label: 'theme.dark' }, { value: 'eye-care', label: 'theme.eye_care' }];
</script>
<style scoped>
.theme-options { align-items: center; }
.theme-option { display: inline-flex; align-items: center; gap: var(--xdev-space-2); }
.theme-swatch { display: inline-flex; width: 22px; height: 22px; overflow: hidden; border: 1px solid var(--xdev-border-strong); border-radius: 50%; }
.theme-swatch i, .theme-swatch b { display: block; width: 50%; height: 100%; }
.theme-swatch--light i { background: var(--xdev-preview-light-accent); } .theme-swatch--light b { background: var(--xdev-preview-light-bg); }
.theme-swatch--dark i { background: var(--xdev-preview-dark-accent); } .theme-swatch--dark b { background: var(--xdev-preview-dark-bg); }
.theme-swatch--eye-care i { background: var(--xdev-preview-eye-care-accent); } .theme-swatch--eye-care b { background: var(--xdev-preview-eye-care-bg); }
</style>
