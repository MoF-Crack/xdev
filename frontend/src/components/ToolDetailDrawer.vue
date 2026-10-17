<template>
  <n-drawer :show="show" placement="right" :width="drawerWidth" :aria-label="tool?.displayName || t('detail.name')" @update:show="$emit('update:show', $event)">
    <n-drawer-content :title="tool?.displayName || ''">
      <div v-if="tool" class="tool-detail-drawer" data-responsive-width="min(520px, calc(100vw - 24px))">
        <n-space vertical>
          <n-descriptions :column="1" bordered size="small">
            <n-descriptions-item :label="t('detail.name')">{{ tool.name }}</n-descriptions-item>
            <n-descriptions-item :label="t('detail.category')">{{ tool.category }}</n-descriptions-item>
            <n-descriptions-item :label="t('detail.description')">{{ tool.description }}</n-descriptions-item>
          </n-descriptions>
          <n-button data-action="favorite" size="small" @click="$emit('toggle-favorite', tool.name)">{{ tool.isFavorite ? t('favorites.remove') : t('favorites.add') }}</n-button>
          <section v-if="tool.installed && (hasGui || hasCli)" class="detail-section" data-action="tool-launch">
            <n-space>
              <n-button v-if="hasGui" data-action="launch" size="small" type="primary" @click="$emit('launch', tool.name)">{{ t('detail.launch') }}</n-button>
              <n-button v-if="hasCli" data-action="copy-command" size="small" @click="$emit('copy-command', tool.name)">{{ t('detail.copy_command') }}</n-button>
            </n-space>
          </section>
          <section v-if="isPackageManager && showPmAction" class="detail-section" data-action="pm-actions">
            <h3>{{ t('pm.actions_title') }}</h3>
            <n-space>
              <n-button v-if="showPmEnable" data-action="package-manager" size="small" type="primary" :loading="pmBusy" :disabled="pmBusy" @click="$emit('install-package-manager', tool)">{{ pmBusy ? t('pm.installing') : t('pm.enable_button') }}</n-button>
              <n-button v-else-if="showPmInstall" data-action="package-manager" size="small" type="primary" :title="tool.requiresAdmin ? t('pm.install_admin_tooltip') : undefined" :loading="pmBusy" :disabled="pmBusy" @click="$emit('install-package-manager', tool)">{{ pmBusy ? t('pm.installing') : (tool.requiresAdmin ? t('pm.install_admin_required') : t('btn.install')) }}</n-button>
              <n-button v-if="showPmUpdate" data-action="package-manager-update" size="small" :disabled="pmBusy" @click="$emit('update-package-manager', tool)">{{ t('btn.update') }}</n-button>
            </n-space>
            <p v-if="tool.installHint && !showPmActionButton" class="pm-hint">{{ tool.installHint }}</p>
          </section>
          <section v-if="tool.localVersion || tool.localPath" class="detail-section">
            <h3>{{ t('detail.local_env') }}</h3>
            <n-tag v-if="tool.localVersion" type="warning">{{ tool.localVersion }}</n-tag>
            <CopyableValue v-if="tool.localPath" :value="tool.localPath" :copy-label="t('btn.copy')" :success-label="t('detail.copied')" :failure-label="t('detail.copy_failed')" />
            <n-space>
              <n-button v-if="tool.localPath && !tool.installed" data-action="migrate" size="tiny" :disabled="migrationBusy" @click="$emit('migrate', tool.name, tool.localPath)">{{ t('detail.migrate_to_xdev') }}</n-button>
              <n-button v-if="tool.localPath && tool.installed" data-action="switch-local" size="tiny" @click="$emit('switch-local', tool.name)">{{ t('detail.switch_to_local') }}</n-button>
            </n-space>
          </section>
          <section v-if="showPmInstances" class="detail-section" data-action="pm-instances">
            <h3>{{ t('pm.instances_title') }}</h3>
            <div v-for="(inst, idx) in tool.instances" :key="idx" class="pm-instance-row">
              <n-tag :type="inst.managed ? 'success' : 'default'" size="small">{{ inst.managed ? t('pm.instance_xdev') : t('pm.instance_local') }}</n-tag>
              <span class="pm-instance-version">{{ inst.version }}</span>
              <CopyableValue :value="inst.path" :copy-label="t('btn.copy')" :success-label="t('detail.copied')" :failure-label="t('detail.copy_failed')" />
              <n-space size="small">
                <n-button v-if="canSwitchInstance(inst)" data-action="switch-instance" size="tiny" :disabled="pmBusy" @click="emitSwitchInstance(inst)">{{ switchLabel(inst) }}</n-button>
                <n-button v-if="!inst.managed" data-action="migrate-pm" size="tiny" :disabled="pmBusy" @click="$emit('migrate-pm', tool.name)">{{ t('pm.migrate') }}</n-button>
              </n-space>
            </div>
          </section>
          <section v-if="deps.length" class="detail-section" data-action="tool-deps">
            <h3>{{ t('detail.deps_title') }}</h3>
            <div class="deps-list">
              <div v-for="dep in deps" :key="dep.tool + (dep.platform || '') + (dep.installPath || '')" class="dep-row" :data-dep="dep.tool">
                <n-tag :type="dep.satisfied ? 'success' : 'warning'" size="small">{{ dep.satisfied ? t('detail.deps_satisfied') : t('detail.deps_missing') }}</n-tag>
                <span class="dep-name">{{ dep.tool }}<template v-if="dep.requiredVersion">{{ t('detail.deps_required', { version: dep.requiredVersion }) }}</template><template v-if="dep.currentVersion">{{ t('detail.deps_current', { version: dep.currentVersion }) }}</template></span>
                <!-- 第七轮审计 F-42：判据必须是 reasonCode，不能是 reason。
                     Go 侧 checkDependency（internal/gui/app_dependencies.go）从不给
                     st.Reason 赋值（该字段是「保留空串」的兼容字段），只填
                     ReasonCode / ReasonParams，因此 v-if="dep.reason" 恒为假 →
                     整条原因行**永不渲染**，用户看不到「PATH 中未找到 node」这类
                     可操作信息。 -->
                <span v-if="dep.reasonCode" class="dep-reason">{{ t('detail.deps_reason_' + dep.reasonCode, dep.reasonParams || {}) }}</span>
                <span v-if="dep.installPath" class="dep-install">{{ t('detail.deps_install_path', { path: dep.installPath }) }}</span>
              </div>
            </div>
          </section>
          <template v-if="!isPackageManager">
            <section class="detail-section">
              <h3>{{ t('detail.installed_versions') }}</h3>
              <VersionList :versions="installedVersions" mode="installed" :t="t" @use="v => $emit('use-version', tool.name, v)" @uninstall="v => $emit('uninstall-version', tool.name, v)" @repair="v => $emit('repair', tool.name, v)" />
            </section>
            <section class="detail-section">
              <h3>{{ t('detail.available_versions') }}</h3>
              <VersionList :versions="availableVersions" mode="available" :limit="showCount" :t="t" @install="v => $emit('install', tool.name, v)" @show-all="$emit('show-more')" />
            </section>
            <section class="detail-section">
              <h3>{{ t('detail.install_custom') }}</h3>
              <n-space>
                <n-input v-model:value="customVersion" />
                <n-button :disabled="!customVersion" @click="$emit('install', tool.name, customVersion)">{{ t('btn.install') }}</n-button>
              </n-space>
            </section>
          </template>
        </n-space>
      </div>
    </n-drawer-content>
  </n-drawer>
</template>
<script setup>
import { computed, nextTick, ref, watch } from 'vue';
import { NButton, NDescriptions, NDescriptionsItem, NDrawer, NDrawerContent, NInput, NSpace, NTag } from 'naive-ui';
import CopyableValue from './CopyableValue.vue';
import VersionList from './VersionList.vue';
const drawerWidth = 'min(520px, calc(100vw - 24px))';
const props = defineProps({
  show: Boolean,
  tool: Object,
  installedVersions: { type: Array, default: () => [] },
  availableVersions: { type: Array, default: () => [] },
  showCount: { type: Number, default: 10 },
  operatingTool: { type: String, default: '' },
  returnFocusTo: Object,
  deps: { type: Array, default: () => [] },
  // R8-02（第八轮审计，P1）：迁移进行中标志，透传自 AppContent 的
  // migratingSingle/migratingAll —— 抽屉里的「迁移到 xdev」按钮此前不受任何约束。
  migrationBusy: Boolean,
  t: { type: Function, required: true },
});
const emit = defineEmits(['update:show', 'toggle-favorite', 'migrate', 'switch-local', 'switch-xdev', 'use-version', 'uninstall-version', 'install', 'repair', 'show-more', 'launch', 'copy-command', 'migrate-pm', 'install-package-manager', 'update-package-manager']);
const hasGui = computed(() => Array.isArray(props.tool?.formFactor) && props.tool.formFactor.includes('gui'));
const hasCli = computed(() => Array.isArray(props.tool?.formFactor) && (props.tool.formFactor.includes('cli') || props.tool.formFactor.includes('tui')));
const isPackageManager = computed(() => props.tool?.category === 'package-manager');
const pmBusy = computed(() => Boolean(props.operatingTool) && props.operatingTool === props.tool?.name);
const showPmEnable = computed(() => isPackageManager.value && props.tool?.canInstall && (!props.tool?.installed || props.tool?.offerSubToolInstall) && props.tool?.enableCommand && !(props.tool?.installMethods && props.tool.installMethods.length > 1));
const showPmInstall = computed(() => isPackageManager.value && props.tool?.canInstall && (!props.tool?.installed || props.tool?.offerSubToolInstall) && !showPmEnable.value);
const showPmUpdate = computed(() => isPackageManager.value && props.tool?.canInstall && props.tool?.installed && props.tool?.hasUpdate);
const showPmActionButton = computed(() => showPmEnable.value || showPmInstall.value || showPmUpdate.value);
const showPmAction = computed(() => showPmActionButton.value || Boolean(props.tool?.installHint));
const showPmInstances = computed(() => Array.isArray(props.tool?.instances) && (props.tool.instances.length > 1 || props.tool.instances.some((i) => !i.managed)));
const customVersion = ref('');
function normalizePath(path) {
  return String(path || '').replace(/\\/g, '/').replace(/\/+$/, '').toLowerCase();
}
function isCurrentInstance(inst) {
  const current = normalizePath(props.tool?.localPath);
  return current !== '' && current === normalizePath(inst?.path);
}
function canSwitchInstance(inst) {
  if (!inst?.path || isCurrentInstance(inst)) return false;
  if (inst.managed) return Boolean(inst.version);
  return true;
}
function switchLabel(inst) {
  return inst.managed ? props.t('pm.switch_to_xdev') : props.t('detail.switch_to_local');
}
function emitSwitchInstance(inst) {
  if (inst.managed) emit('switch-xdev', props.tool.name, inst.version);
  else emit('switch-local', props.tool.name, inst.path);
}
watch(() => props.tool?.name, () => { customVersion.value = ''; });
watch(() => props.show, async (show, was) => {
  if (!show) customVersion.value = '';
  if (!show && was) {
    await nextTick();
    props.returnFocusTo?.focus?.();
  }
});
</script>
<style scoped>
.detail-section { padding: var(--xdev-space-3); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-md); }
h3 { margin: 0 0 var(--xdev-space-3); color: var(--xdev-text-muted); font-size: 11px; font-weight: 600; letter-spacing: 0.1em; text-transform: uppercase; }
.deps-list { display: grid; gap: var(--xdev-space-2); }
.dep-row { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: var(--xdev-space-2); align-items: center; }
.dep-reason, .dep-install { grid-column: 1 / -1; color: var(--xdev-text-muted); font-size: var(--xdev-font-size-xs); }
.pm-instance-row { display: grid; grid-template-columns: auto auto minmax(0, 1fr) auto; gap: var(--xdev-space-2); align-items: center; margin-bottom: var(--xdev-space-2); }
.pm-instance-version { font-weight: 600; color: var(--xdev-text-secondary); }
.pm-hint { margin: var(--xdev-space-2) 0 0; color: var(--xdev-text-muted); font-size: var(--xdev-font-size-xs); }
</style>
