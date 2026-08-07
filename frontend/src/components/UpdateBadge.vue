<template>
  <n-popover trigger="click" placement="bottom-end" :show-arrow="false" style="width: 360px; max-height: 420px; overflow: auto;">
    <template #trigger>
      <n-badge :value="count" :max="99" :show-zero="false">
        <n-button size="small" secondary :aria-label="t('updates.title')">
          <template #icon><n-icon aria-hidden="true"><arrow-up-circle-outline /></n-icon></template>
          {{ count > 0 ? t('updates.count', { n: count }) : t('updates.title') }}
        </n-button>
      </n-badge>
    </template>
    <n-space vertical>
      <div class="updates-popover-header">
        <strong>{{ t('updates.title') }}</strong>
        <n-button size="tiny" secondary :loading="loading" @click="$emit('refresh')">{{ t('btn.refresh') }}</n-button>
      </div>
      <n-empty v-if="!updates?.length" :description="t('updates.empty')" />
      <n-list v-else size="small">
        <n-list-item v-for="u in updates" :key="u.tool">
          <div class="update-item">
            <div><strong>{{ u.tool }}</strong><div class="update-version">{{ u.currentVersion }} → {{ u.latestVersion }}</div></div>
            <div class="update-actions">
              <n-button size="tiny" type="primary" :loading="installing[u.tool]" @click="$emit('install', u)">{{ t('updates.install') }}</n-button>
              <n-button size="tiny" secondary @click="$emit('ignore', u)">{{ t('updates.ignore') }}</n-button>
            </div>
          </div>
        </n-list-item>
      </n-list>
      <n-divider style="margin: 8px 0;" />
      <div class="updates-popover-actions"><n-button size="tiny" secondary :loading="sniffing" @click="$emit('sniff-all')">{{ t('updates.sniff_all') }}</n-button></div>
    </n-space>
  </n-popover>
</template>

<script setup>
import { ArrowUpCircleOutline } from '@vicons/ionicons5';
import { NBadge, NButton, NEmpty, NIcon, NList, NListItem, NPopover, NSpace, NDivider } from 'naive-ui';
defineProps({ t: { type: Function, required: true }, count: { type: Number, default: 0 }, updates: { type: Array, default: () => [] }, loading: Boolean, sniffing: Boolean, installing: { type: Object, default: () => ({}) } });
defineEmits(['refresh', 'install', 'ignore', 'sniff-all']);
</script>
<style scoped>
.updates-popover-header { display: flex; align-items: center; justify-content: space-between; }
.update-item { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.update-actions { display: flex; align-items: center; gap: 6px; flex-shrink: 0; }
.update-version { color: var(--xdev-text-muted); font-size: 12px; }
.updates-popover-actions { display: flex; justify-content: flex-end; }
</style>
