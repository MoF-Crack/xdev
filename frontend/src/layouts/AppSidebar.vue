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
.app-sidebar { width: 216px; min-width: 216px; display: flex; flex-direction: column; overflow-y: auto; border-right: 1px solid var(--xdev-border-subtle); background: var(--xdev-bg-panel); transition: width var(--xdev-motion-normal) var(--xdev-ease-standard), min-width var(--xdev-motion-normal) var(--xdev-ease-standard); }
.app-sidebar--compact { width: 56px; min-width: 56px; }
.app-sidebar__brand { min-height: 64px; display: flex; align-items: center; gap: var(--xdev-space-2); padding: 0 var(--xdev-space-4); border-bottom: 1px solid var(--xdev-border-subtle); }
.app-sidebar__brand strong { color: var(--xdev-text-primary); font-size: var(--xdev-font-size-md); font-weight: 650; letter-spacing: 0.01em; }
.app-sidebar__brand :deep(svg) { width: 26px; height: 26px; flex: 0 0 26px; }
nav { flex: 1; padding: var(--xdev-space-4) var(--xdev-space-3); }
section { margin-bottom: var(--xdev-space-3); }
.app-sidebar__group { margin: var(--xdev-space-2) var(--xdev-space-3); color: var(--xdev-text-muted); font-size: 11px; font-weight: 600; letter-spacing: 0.1em; text-transform: uppercase; }
button { width: 100%; min-height: 36px; display: flex; align-items: center; gap: var(--xdev-space-3); margin: 2px 0; padding: 0 var(--xdev-space-3); border: 0; border-radius: var(--xdev-radius-sm); background: transparent; color: var(--xdev-text-secondary); cursor: pointer; text-align: left; transition: background var(--xdev-motion-fast) var(--xdev-ease-standard), color var(--xdev-motion-fast) var(--xdev-ease-standard); }
button svg { width: 18px; height: 18px; flex: 0 0 18px; }
button:hover { background: var(--xdev-bg-hover); color: var(--xdev-text-primary); }
button.active { background: var(--xdev-accent-soft); color: var(--xdev-accent); box-shadow: inset 2px 0 0 var(--xdev-accent); font-weight: 600; }
[data-theme="light"] button.active, [data-theme="eye-care"] button.active { color: var(--xdev-accent-pressed); }
button:focus-visible { background: var(--xdev-accent-soft); color: var(--xdev-text-primary); }
.app-sidebar__toggle { margin: var(--xdev-space-3); width: calc(100% - var(--xdev-space-6)); border-top: 1px solid var(--xdev-border-subtle); border-radius: 0; }
@media (max-width: 799px) { .app-sidebar { width: 56px; min-width: 56px; } .app-sidebar strong, .app-sidebar__group, .app-sidebar button span:not([aria-hidden]) { display: none; } }
</style>
