<template>
  <div v-if="show" class="command-palette-backdrop" @mousedown.self="close">
    <section class="command-palette" role="dialog" aria-modal="true" :aria-label="title">
      <div class="command-palette__search" :class="{ 'command-palette__search--focused': searchFocused }">
        <SearchOutline aria-hidden="true" />
        <input
          ref="inputRef"
          v-model="query"
          role="combobox"
          type="search"
          autocomplete="off"
          aria-autocomplete="list"
          aria-controls="command-palette-listbox"
          :aria-activedescendant="activeOptionId"
          :aria-expanded="show"
          :aria-label="searchLabel"
          :placeholder="searchLabel"
          @focus="searchFocused = true"
          @blur="searchFocused = false"
          @keydown="onKeydown"
        >
      </div>

      <div id="command-palette-listbox" class="command-list" role="listbox" :aria-label="title">
        <button
          v-for="(item, index) in filteredOptions"
          :id="optionId(item)"
          :key="item.key"
          class="xdev-btn command-item xdev-btn--secondary"
          :class="{ 'command-item--active': index === activeIndex }"
          type="button"
          role="option"
          tabindex="-1"
          :aria-selected="index === activeIndex"
          @click="select(item)"
          @pointerenter="activeIndex = index"
          @pointermove="activeIndex = index"
          @mousemove="activeIndex = index"
        >
          <span class="command-label">{{ item.label }}</span>
          <span class="command-meta">{{ item.group }}</span>
        </button>
      </div>
      <p v-if="filteredOptions.length === 0" class="command-empty" role="status">{{ emptyLabel }}</p>
    </section>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue';
import { SearchOutline } from '@vicons/ionicons5';

const props = defineProps({
  show: { type: Boolean, default: false },
  options: { type: Array, default: () => [] },
  title: { type: String, required: true },
  searchLabel: { type: String, required: true },
  emptyLabel: { type: String, required: true },
});

const emit = defineEmits(['update:show', 'select']);
const query = ref('');
const inputRef = ref(null);
const activeIndex = ref(0);
const searchFocused = ref(false);
const filteredOptions = computed(() => {
  const normalizedQuery = query.value.trim().toLowerCase();
  if (!normalizedQuery) return props.options.slice(0, 12);
  return props.options
    .filter((item) => `${item.label} ${item.group} ${item.key}`.toLowerCase().includes(normalizedQuery))
    .slice(0, 12);
});
const activeOptionId = computed(() => filteredOptions.value[activeIndex.value] ? optionId(filteredOptions.value[activeIndex.value]) : undefined);

function optionId(item) { return `command-option-${item.key.replace(/[^a-zA-Z0-9_-]/g, '-')}`; }
function close() { emit('update:show', false); }
function select(item) {
  emit('select', item.key);
  close();
  query.value = '';
}
function moveActive(delta) {
  const count = filteredOptions.value.length;
  if (count) activeIndex.value = (activeIndex.value + delta + count) % count;
}
function onKeydown(event) {
  if (event.key === 'ArrowDown') moveActive(1);
  else if (event.key === 'ArrowUp') moveActive(-1);
  else if (event.key === 'Home') activeIndex.value = 0;
  else if (event.key === 'End') activeIndex.value = Math.max(0, filteredOptions.value.length - 1);
  else if (event.key === 'Enter' && filteredOptions.value[activeIndex.value]) select(filteredOptions.value[activeIndex.value]);
  else if (event.key === 'Escape') close();
  else if (event.key === 'Tab') inputRef.value?.focus();
  else return;
  event.preventDefault();
}
function keepFocusInside(event) {
  if (props.show && event.target !== inputRef.value) inputRef.value?.focus();
}

watch(filteredOptions, () => { activeIndex.value = 0; });
watch(() => props.show, (visible) => {
  document.removeEventListener('focusin', keepFocusInside);
  if (visible) {
    activeIndex.value = 0;
    document.addEventListener('focusin', keepFocusInside);
    inputRef.value?.focus();
  }
}, { flush: 'post', immediate: true });
onBeforeUnmount(() => document.removeEventListener('focusin', keepFocusInside));
</script>

<style scoped>
.command-palette-backdrop { position: fixed; inset: 0; z-index: 3000; display: grid; place-items: start center; padding-top: min(18vh, 140px); background: var(--xdev-overlay); }
.command-palette { width: min(680px, calc(100vw - 40px)); padding: 12px; border: 1px solid var(--xdev-border-strong); border-radius: var(--xdev-radius-md); background: var(--xdev-bg-panel); box-shadow: var(--xdev-shadow-soft); }
.command-palette__search { display: flex; align-items: center; gap: var(--xdev-space-2); padding: 0 var(--xdev-space-3); border: 1px solid var(--xdev-border-strong); border-radius: var(--xdev-radius-md); color: var(--xdev-text-muted); }
.command-palette__search svg { width: 20px; flex: none; }
.command-palette__search input { width: 100%; min-height: 44px; border: 0; outline: 0; background: transparent; color: var(--xdev-text-primary); font: inherit; }
.command-palette__search--focused, .command-palette__search:focus-within { box-shadow: var(--xdev-focus-ring); }
.command-list { max-height: min(420px, calc(100vh - 180px)); margin-top: 10px; overflow: auto; }
.command-item { width: 100%; min-height: 42px; display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 8px 10px; border: 0; border-radius: var(--xdev-radius-sm); background: transparent; color: var(--xdev-text-primary); cursor: pointer; text-align: left; }
.command-item--active { background: var(--xdev-accent-soft); }
.command-label { overflow: hidden; font-size: 14px; text-overflow: ellipsis; white-space: nowrap; }
  .command-meta { color: var(--xdev-text-secondary); font-size: 12px; white-space: nowrap; }
.command-empty { margin: var(--xdev-space-5) 0; color: var(--xdev-text-muted); text-align: center; }
</style>
