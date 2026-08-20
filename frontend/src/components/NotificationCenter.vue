<template>
  <n-popover trigger="click" placement="bottom-end" :show-arrow="false" style="width: 320px; max-height: 360px; overflow: auto;" @update:show="onShowChange">
    <template #trigger>
      <n-badge dot :show="unreadCount > 0">
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
        <n-list-item v-for="n in notifications" :key="n.id || n.time">
          <div class="notification-item">
            <n-icon aria-hidden="true" :color="n.type === 'error' ? 'var(--xdev-error)' : 'var(--xdev-success)'">
              <checkmark-circle-outline v-if="n.type !== 'error'" />
              <alert-circle-outline v-else />
            </n-icon>
            <div>
              <div class="notification-title">{{ n.title }}</div>
              <div class="notification-time">{{ n.time }}</div>
            </div>
            <n-button size="tiny" text class="notification-dismiss" :aria-label="t('notifications.dismiss')" @click.stop="$emit('dismiss', n.id)">{{ t('notifications.dismiss') }}</n-button>
          </div>
        </n-list-item>
      </n-list>
    </n-space>
  </n-popover>
</template>

<script setup>
import { computed, ref, watch } from 'vue';
import { NotificationsOutline, CheckmarkCircleOutline, AlertCircleOutline } from '@vicons/ionicons5';
import { NBadge, NButton, NEmpty, NIcon, NList, NListItem, NPopover, NSpace } from 'naive-ui';

const props = defineProps({
  t: { type: Function, required: true },
  notifications: { type: Array, default: () => [] },
});
const emit = defineEmits(['clear', 'dismiss']);

const seenIds = ref(new Set());
const unreadCount = computed(() => props.notifications.filter((n) => n.id && !seenIds.value.has(n.id)).length);

function markAllSeen() {
  const next = new Set(seenIds.value);
  for (const n of props.notifications) {
    if (n.id) next.add(n.id);
  }
  seenIds.value = next;
}

function onShowChange(show) {
  if (show) {
    markAllSeen();
    return;
  }
  if (props.notifications.length) emit('clear');
}

watch(() => props.notifications.map((n) => n.id).join('|'), () => {
  // Drop ids that are no longer in the list so the Set cannot grow forever.
  const live = new Set(props.notifications.map((n) => n.id).filter(Boolean));
  seenIds.value = new Set([...seenIds.value].filter((id) => live.has(id)));
});

defineExpose({ unreadCount });
</script>

<style scoped>
.notification-header { display: flex; align-items: center; justify-content: space-between; }
.notification-item { display: flex; align-items: flex-start; gap: 8px; }
.notification-title { font-size: 13px; }
.notification-time { color: var(--xdev-text-muted); font-size: 11px; }
.notification-dismiss { margin-left: auto; }
</style>
