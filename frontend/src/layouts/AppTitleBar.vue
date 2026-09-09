<template>
  <header data-testid="title-bar" class="title-bar" @dblclick="toggleMaximize">
    <div class="title-bar__identity"><XDevLogo decorative /><span>XDev</span></div>
    <div class="title-bar__controls" @dblclick.stop>
      <button type="button" class="title-bar__button" :aria-label="minimizeLabel" :title="minimizeLabel" @click.stop="minimize" @dblclick.stop><span aria-hidden="true">−</span></button>
      <button type="button" class="title-bar__button" :aria-label="maximizeLabel" :title="maximizeLabel" @click.stop="toggleMaximize" @dblclick.stop><span aria-hidden="true">□</span></button>
      <button type="button" class="title-bar__button title-bar__close" :aria-label="closeLabel" :title="closeLabel" @click.stop="close" @dblclick.stop><span aria-hidden="true">×</span></button>
    </div>
  </header>
</template>

<script setup>
import XDevLogo from '../components/XDevLogo.vue';
defineProps({
  minimize: { type: Function, required: true }, toggleMaximize: { type: Function, required: true }, close: { type: Function, required: true },
  minimizeLabel: { type: String, default: 'Minimize' }, maximizeLabel: { type: String, default: 'Maximize' }, closeLabel: { type: String, default: 'Close' },
});
</script>

<style scoped>
.title-bar { height: 34px; display: flex; align-items: center; justify-content: space-between; background: var(--xdev-bg-app); border-bottom: 1px solid var(--xdev-border-subtle); --wails-draggable: drag; user-select: none; }
.title-bar__identity { display: flex; align-items: center; gap: var(--xdev-space-2); padding-left: var(--xdev-space-3); color: var(--xdev-text-secondary); font-size: var(--xdev-font-size-sm); font-weight: 600; letter-spacing: 0.06em; }
.title-bar__identity :deep(svg) { width: 16px; height: 16px; }
.title-bar__controls { height: 100%; display: flex; --wails-draggable: no-drag; }
button { width: 46px; height: 100%; border: 0; background: transparent; color: var(--xdev-text-muted); cursor: pointer; font: inherit; font-size: 14px; transition: background var(--xdev-motion-fast) var(--xdev-ease-standard), color var(--xdev-motion-fast) var(--xdev-ease-standard); }
button:hover { background: var(--xdev-bg-hover); color: var(--xdev-text-primary); }
button:focus-visible { background: var(--xdev-bg-hover); color: var(--xdev-text-primary); }
.title-bar__close:hover, .title-bar__close:focus-visible { background: var(--xdev-error); color: var(--xdev-text-on-accent); }
</style>
