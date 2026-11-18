<template>
  <div class="app-shell" :class="{ 'app-shell--compact': compact }">
    <AppTitleBar v-bind="titleBarProps" />
    <div class="app-shell__body">
      <AppSidebar :groups="navigationGroups" :selected-view="selectedView" :compact="compact" :narrow="narrow" :t="t" @select="$emit('select-view', $event)" @toggle-compact="userCompact = !userCompact" />
      <div class="app-shell__main">
        <slot name="toolbar" />
        <div class="app-shell__workspace" :class="{ 'app-shell__workspace--split': inspectorOpen }"><div class="app-shell__content"><slot /></div><div v-if="inspectorOpen" class="app-shell__inspector"><slot name="inspector" /></div></div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue';
import AppSidebar from './AppSidebar.vue';
import AppTitleBar from './AppTitleBar.vue';

const props = defineProps({
  inspectorOpen: Boolean,
  selectedView: { type: String, required: true }, navigationGroups: { type: Array, required: true },
  t: { type: Function, required: true },
  minimize: { type: Function, required: true }, toggleMaximize: { type: Function, required: true }, close: { type: Function, required: true },
  minimizeLabel: { type: String, default: 'Minimize' }, maximizeLabel: { type: String, default: 'Maximize' }, closeLabel: { type: String, default: 'Close' },
});
const emit = defineEmits(['select-view', 'open-command-palette', 'escape']);
const userCompact = ref(false);
const narrow = ref(false);
const compact = computed(() => narrow.value || userCompact.value);
const titleBarProps = computed(() => ({ minimize: props.minimize, toggleMaximize: props.toggleMaximize, close: props.close, minimizeLabel: props.minimizeLabel, maximizeLabel: props.maximizeLabel, closeLabel: props.closeLabel }));
let narrowMedia;
function updateNarrow(event) { narrow.value = event.matches; }
function onKeydown(event) {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') { event.preventDefault(); emit('open-command-palette'); }
  else if (event.key === 'Escape') emit('escape');
}
onMounted(() => {
  window.addEventListener('keydown', onKeydown);
  narrowMedia = window.matchMedia('(max-width: 799px)');
  updateNarrow(narrowMedia);
  narrowMedia.addEventListener('change', updateNarrow);
});
onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown);
  narrowMedia?.removeEventListener('change', updateNarrow);
});
</script>

<style scoped>
.app-shell { height: 100vh; overflow: hidden; background: var(--xdev-bg-app); }
.app-shell__body { height: calc(100vh - 34px); display: flex; min-width: 0; }
.app-shell__main { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.app-shell__workspace { display: flex; flex: 1; min-width: 0; min-height: 0; overflow: hidden; }
.app-shell__inspector { width: 350px; flex: 0 0 350px; min-height: 0; margin: var(--xdev-page-top-space) var(--xdev-content-padding) 24px 0; overflow: auto; border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-lg); background: var(--xdev-bg-card); }
.app-shell__workspace--split .app-shell__content { padding-right: 20px; }
.app-shell__content { flex: 1; min-width: 0; overflow: auto; padding: var(--xdev-page-top-space) var(--xdev-content-padding) 56px; scrollbar-gutter: stable; }
@media (max-width: 799px) { .app-shell__content { padding: var(--xdev-space-5) var(--xdev-space-4) 40px; } }
</style>
