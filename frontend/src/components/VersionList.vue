<template>
  <div class="version-list">
    <p v-if="!versions.length" class="version-list__empty">{{ emptyLabel }}</p>
    <div v-for="version in displayedVersions" :key="version.version" class="version-row">
      <n-space align="center" justify="space-between">
        <n-space align="center"><n-tag :type="version.active || version.installed ? 'success' : 'default'" size="small">{{ version.version }}</n-tag><n-tag v-if="version.active" size="tiny" type="success">{{ t('detail.active') }}</n-tag><n-tag v-if="mode === 'installed' && version.intact === false" size="tiny" type="warning">{{ t('detail.incomplete') }}</n-tag></n-space>
        <n-space>
          <n-button v-if="mode === 'installed' && version.intact === false" data-action="repair-version" size="tiny" type="warning" @click="$emit('repair', version.version)">{{ t('detail.repair') }}</n-button>
          <n-button v-if="mode === 'installed' && !version.active" data-action="use-version" size="tiny" type="primary" @click="$emit('use', version.version)">{{ t('btn.use') }}</n-button>
          <n-button v-if="mode === 'installed'" data-action="uninstall-version" size="tiny" type="error" ghost @click="$emit('uninstall', version.version)">{{ t('btn.remove') }}</n-button>
          <n-button v-if="mode === 'available' && !version.installed" data-action="install-version" size="tiny" type="primary" ghost @click="$emit('install', version.version)">{{ t('btn.install') }}</n-button>
          <n-tag v-if="mode === 'available' && version.installed" size="tiny" type="success">{{ t('status.installed') }}</n-tag>
        </n-space>
      </n-space>
    </div>
    <n-button v-if="limit && versions.length > limit" data-action="show-all" text type="primary" size="small" @click="$emit('show-all')">{{ t('detail.show_more', { count: versions.length - limit }) }}</n-button>
  </div>
</template>
<script setup>
import { computed } from 'vue';
import { NButton, NSpace, NTag } from 'naive-ui';
const props = defineProps({ versions: { type: Array, default: () => [] }, mode: { type: String, default: 'available' }, limit: { type: Number, default: 0 }, emptyLabel: { type: String, default: '' }, t: { type: Function, required: true } });
defineEmits(['use', 'uninstall', 'install', 'repair', 'show-all']);
const displayedVersions = computed(() => props.limit ? props.versions.slice(0, props.limit) : props.versions);
</script>
<style scoped>.version-list{display:grid;gap:8px}.version-row{padding:6px 0;border-bottom:1px solid var(--xdev-border-subtle)}.version-list__empty{color:var(--xdev-text-muted);font-size:12px}</style>
