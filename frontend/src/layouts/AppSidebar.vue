<template>
  <aside class="app-sidebar" :class="{ 'app-sidebar--compact': compact }">
    <div class="app-sidebar__brand"><XDevLogo :decorative="!compact" /><strong v-if="!compact">XDev</strong></div>
    <nav aria-label="Application navigation">
      <section v-for="group in groups" :key="group.key">
        <p v-if="!compact" class="app-sidebar__group">{{ group.label }}</p>
        <button v-for="page in group.items" :key="page.key" type="button" :data-view="page.key" :class="{ active: page.key === selectedView }" :aria-current="page.key === selectedView ? 'page' : undefined" :aria-label="page.label" :title="compact ? page.label : undefined" @click="$emit('select', page.key)">
          <component :is="page.icon" v-if="page.icon" aria-hidden="true" />
          <span v-if="!compact">{{ page.label }}</span>
        </button>
      </section>
    </nav>
    <button v-if="!narrow" type="button" class="app-sidebar__toggle" :aria-label="compact ? 'Expand sidebar' : 'Collapse sidebar'" @click="$emit('toggle-compact')">
      <span aria-hidden="true">{{ compact ? '›' : '‹' }}</span><span v-if="!compact">Collapse</span>
    </button>
  </aside>
</template>

<script setup>
import XDevLogo from '../components/XDevLogo.vue';
defineProps({ groups: { type: Array, required: true }, selectedView: { type: String, required: true }, compact: Boolean, narrow: Boolean });
defineEmits(['select', 'toggle-compact']);
</script>

<style scoped>
.app-sidebar { width: 200px; min-width: 200px; display: flex; flex-direction: column; overflow-y: auto; border-right: 1px solid var(--xdev-border-subtle); background: var(--xdev-bg-panel); transition: width var(--xdev-motion-normal) var(--xdev-ease-standard), min-width var(--xdev-motion-normal) var(--xdev-ease-standard); }
.app-sidebar--compact { width: 56px; min-width: 56px; }
.app-sidebar__brand { min-height: 56px; display: flex; align-items: center; gap: var(--xdev-space-2); padding: 0 14px; }
.app-sidebar__brand :deep(svg) { width: 28px; height: 28px; flex: 0 0 28px; }
nav { flex: 1; padding: 0 var(--xdev-space-2); }
section { margin-bottom: var(--xdev-space-3); }
.app-sidebar__group { margin: var(--xdev-space-2); color: var(--xdev-text-muted); font-size: var(--xdev-font-size-xs); text-transform: uppercase; }
button { width: 100%; min-height: 36px; display: flex; align-items: center; gap: var(--xdev-space-3); padding: 0 10px; border: 0; border-radius: var(--xdev-radius-sm); background: transparent; color: var(--xdev-text-secondary); cursor: pointer; text-align: left; }
button svg { width: 18px; height: 18px; flex: 0 0 18px; }
button:hover, button.active { background: var(--xdev-accent-soft); color: var(--xdev-text-primary); }
button:focus-visible { background: var(--xdev-accent-soft); color: var(--xdev-text-primary); outline: 2px solid var(--xdev-focus-ring); outline-offset: -2px; }
.app-sidebar__toggle { margin: var(--xdev-space-2); width: calc(100% - var(--xdev-space-4)); border-top: 1px solid var(--xdev-border-subtle); }
@media (max-width: 799px) { .app-sidebar { width: 56px; min-width: 56px; } .app-sidebar strong, .app-sidebar__group, .app-sidebar button span:not([aria-hidden]) { display: none; } }
</style>
