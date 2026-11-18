<template>
  <div class="quick-theme-switch" role="group" :aria-label="t('settings.theme')">
    <button v-for="option in primaryThemes" :key="option.value" type="button" :data-theme-select="option.value" :aria-pressed="theme === option.value" :title="t(option.labelKey)" @click="$emit('select', option.value)">
      <component :is="icons[option.value]" aria-hidden="true" /><span>{{ t(option.labelKey) }}</span>
    </button>
  </div>
</template>
<script setup>
import { LeafOutline, MoonOutline, SunnyOutline } from '@vicons/ionicons5';
import { THEMES } from '../theme/themes.js';
defineProps({ theme: { type: String, required: true }, t: { type: Function, required: true } });
defineEmits(['select']);
const icons = { 'eye-care': LeafOutline, light: SunnyOutline, dark: MoonOutline };
const primaryThemes = ['eye-care', 'light', 'dark'].map(value => THEMES.find(theme => theme.value === value)).filter(Boolean);
</script>
<style scoped>
.quick-theme-switch { display:flex; gap:2px; padding:3px; border:1px solid var(--xdev-border-subtle); border-radius:9px; background:var(--xdev-bg-app); }
button { display:flex; align-items:center; justify-content:center; gap:5px; min-height:28px; padding:3px 8px; border:0; border-radius:6px; background:transparent; color:var(--xdev-text-secondary); font:inherit; font-size:11px; cursor:pointer; }
button svg { width:14px; height:14px; }
button[aria-pressed="true"] { color:var(--xdev-accent); background:var(--xdev-bg-card); box-shadow:var(--xdev-shadow-sm); font-weight:600; }
button:hover { color:var(--xdev-accent); }
@media(max-width:1100px) { button span { display:none; } button { width:29px; padding:3px; } }
</style>
