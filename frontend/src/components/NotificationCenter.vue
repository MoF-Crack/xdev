<template>
  <n-popover trigger="click" placement="bottom-end" :show-arrow="false" style="width: 320px; max-height: 360px; overflow: auto;">
    <template #trigger>
      <n-badge dot :show="notifications.length > 0">
        <n-button size="small" secondary circle :aria-label="t('notifications.title')">
          <template #icon><n-icon aria-hidden="true"><notifications-outline /></n-icon></template>
        </n-button>
      </n-badge>
    </template>
    <n-space vertical>
      <div class="notification-header">
        <strong>{{ t('notifications.title') }}</strong>
        <n-button size="tiny" text @click="$emit('clear')">{{ t('notifications.clear') }}</n-button>
      </div>
      <n-empty v-if="!notifications.length" :description="t('notifications.empty')" />
      <n-list v-else size="small">
        <n-list-item v-for="(n, idx) in notifications" :key="idx">
          <div class="notification-item">
            <n-icon aria-hidden="true" :color="n.type === 'error' ? 'var(--xdev-error)' : 'var(--xdev-success)'">
              <checkmark-circle-outline v-if="n.type !== 'error'" />
              <alert-circle-outline v-else />
            </n-icon>
            <div>
              <div class="notification-title">{{ n.title }}</div>
              <div class="notification-time">{{ n.time }}</div>
            </div>
          </div>
        </n-list-item>
      </n-list>
    </n-space>
  </n-popover>
</template>

<script setup>
import { NotificationsOutline, CheckmarkCircleOutline, AlertCircleOutline } from '@vicons/ionicons5';
import { NBadge, NButton, NEmpty, NIcon, NList, NListItem, NPopover, NSpace } from 'naive-ui';

defineProps({
  t: { type: Function, required: true },
  notifications: { type: Array, default: () => [] },
});
defineEmits(['clear']);
</script>

<style scoped>
.notification-header { display: flex; align-items: center; justify-content: space-between; }
.notification-item { display: flex; align-items: flex-start; gap: 8px; }
.notification-title { font-size: 13px; }
.notification-time { color: var(--xdev-text-muted); font-size: 11px; }
</style>
