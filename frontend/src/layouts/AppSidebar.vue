<template>
  <aside class="app-sidebar" :class="{ 'app-sidebar--compact': compact }">
    <div class="app-sidebar__brand"><XDevLogo :decorative="!compact" /><strong v-if="!compact">XDev</strong></div>
    <nav :aria-label="t('nav.application_navigation')">
      <section v-for="group in groups" :key="group.key">
        <p v-if="!compact" class="app-sidebar__group">{{ group.label }}</p>
        <button v-for="page in group.items" :key="page.key" type="button" class="xdev-btn-nav" :data-view="page.key" :class="{ active: page.key === selectedView }" :aria-current="page.key === selectedView ? 'page' : undefined" :aria-label="page.label" :title="compact ? page.label : undefined" @click="$emit('select', page.key)">
          <component :is="page.icon" v-if="page.icon" aria-hidden="true" />
          <span v-if="!compact">{{ page.label }}</span>
        </button>
      </section>
    </nav>
    <button v-if="!narrow" type="button" class="xdev-btn-nav app-sidebar__toggle" :aria-label="compact ? t('nav.expand_sidebar') : t('nav.collapse_sidebar')" @click="$emit('toggle-compact')">
      <span aria-hidden="true">{{ compact ? '›' : '‹' }}</span><span v-if="!compact">{{ t('nav.collapse') }}</span>
    </button>
  </aside>
</template>

<script setup>
import XDevLogo from '../components/XDevLogo.vue';
defineProps({ groups: { type: Array, required: true }, selectedView: { type: String, required: true }, compact: Boolean, narrow: Boolean, t: { type: Function, required: true } });
defineEmits(['select', 'toggle-compact']);
</script>

<style scoped>
.app-sidebar { width: 216px; min-width: 216px; display: flex; flex-direction: column; overflow-y: auto; border-right: 1px solid var(--xdev-border-subtle); background: color-mix(in srgb, var(--xdev-bg-panel) 92%, var(--xdev-bg-app)); box-shadow: 8px 0 30px color-mix(in srgb, var(--xdev-accent-soft) 22%, transparent); transition: width var(--xdev-motion-normal) var(--xdev-ease-standard), min-width var(--xdev-motion-normal) var(--xdev-ease-standard); }
.app-sidebar--compact { width: 56px; min-width: 56px; }
.app-sidebar__brand { min-height: 72px; display: flex; align-items: center; gap: var(--xdev-space-2); padding: 0 var(--xdev-space-4); border-bottom: 1px solid var(--xdev-border-subtle); }
.app-sidebar__brand :deep(svg) { width: 28px; height: 28px; flex: 0 0 28px; }
nav { flex: 1; padding: var(--xdev-space-5) var(--xdev-space-3); }
section { margin-bottom: var(--xdev-space-3); }
.app-sidebar__group { margin: var(--xdev-space-2); color: var(--xdev-text-secondary); font-size: var(--xdev-font-size-xs); text-transform: uppercase; }
button { width: 100%; min-height: 40px; display: flex; align-items: center; gap: var(--xdev-space-3); margin: 3px 0; padding: 0 var(--xdev-space-3); border: 0; border-radius: var(--xdev-radius-md); background: transparent; color: var(--xdev-text-secondary); cursor: pointer; text-align: left; transition: background var(--xdev-motion-fast), color var(--xdev-motion-fast), transform var(--xdev-motion-fast); }
button svg { width: 18px; height: 18px; flex: 0 0 18px; }
button:hover { background: var(--xdev-bg-elevated); color: var(--xdev-text-primary); transform: translateX(2px); }
button.active { background: var(--xdev-accent-soft); color: var(--xdev-accent); box-shadow: inset 3px 0 0 var(--xdev-accent); }
[data-theme="light"] button.active { color: var(--xdev-accent-pressed); }
button:focus-visible { background: var(--xdev-accent-soft); color: var(--xdev-text-primary); outline: 2px solid var(--xdev-focus-ring); outline-offset: -2px; }
.app-sidebar__toggle { margin: var(--xdev-space-3); width: calc(100% - var(--xdev-space-6)); border-top: 1px solid var(--xdev-border-subtle); border-radius: 0; }
@media (max-width: 799px) { .app-sidebar { width: 56px; min-width: 56px; } .app-sidebar strong, .app-sidebar__group, .app-sidebar button span:not([aria-hidden]) { display: none; } }
</style>
