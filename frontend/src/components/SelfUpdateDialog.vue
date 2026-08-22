<template>
  <div v-if="show" class="self-update-modal" data-testid="self-update-dialog">
    <div class="self-update-mask" />
    <section class="self-update-panel" role="dialog" aria-modal="true" tabindex="-1" :aria-label="t('self_update.title')">
      <h2>{{ t('self_update.title') }}</h2>
      <p class="self-update__text">{{ t('self_update.text', { current, latest }) }}</p>
      <div class="dialog-actions">
        <n-button data-action="self-update-quit" @click="$emit('quit')">{{ t('self_update.quit') }}</n-button>
        <n-button data-action="self-update-ignore" secondary @click="$emit('ignore')">{{ t('self_update.ignore') }}</n-button>
        <n-button data-action="self-update-update" type="primary" :loading="updating" @click="$emit('update')">{{ t('btn.update') }}</n-button>
      </div>
    </section>
  </div>
</template>

<script setup>
import { NButton } from 'naive-ui';

// Spec 034: 启动自检三选对话框（更新 / 忽略并继续 / 退出）。
defineProps({
  show: Boolean,
  current: { type: String, default: '' },
  latest: { type: String, default: '' },
  updating: Boolean,
  t: { type: Function, required: true },
});
defineEmits(['update', 'ignore', 'quit']);
</script>

<style scoped>
.self-update-modal { position: fixed; inset: 0; z-index: 3000; display: grid; place-items: center; }
.self-update-mask { position: absolute; inset: 0; background: var(--xdev-overlay); }
.self-update-panel { position: relative; width: min(440px, calc(100vw - 24px)); padding: var(--xdev-space-5); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-lg); background: var(--xdev-bg-panel); box-shadow: var(--xdev-shadow-lg); color: var(--xdev-text-primary); }
.self-update-panel:focus-visible, .self-update-panel:focus-within { box-shadow: var(--xdev-shadow-lg), var(--xdev-focus-ring); }
h2 { margin: 0 0 var(--xdev-space-4); font-size: var(--xdev-font-size-lg); }
.self-update__text { margin: 0 0 var(--xdev-space-4); line-height: 1.6; }
.dialog-actions { display: flex; align-items: center; justify-content: flex-end; gap: var(--xdev-space-3); flex-wrap: wrap; }
.dialog-actions :deep(.n-button) { min-width: 96px; white-space: nowrap; }
</style>