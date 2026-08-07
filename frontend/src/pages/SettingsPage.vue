<template>
  <div class="settings-page">
    <nav :aria-label="t('settings.sections')">
      <a href="#appearance">{{ t('settings.theme') }}</a>
      <a href="#network">{{ t('settings.mirror_config') }}</a>
      <a href="#custom-tools">{{ t('custom_tools.title') }}</a>
      <a href="#discovery">{{ t('updates.sniff_strategy') }}</a>
      <a href="#maintenance">{{ t('settings.cache_management') }}</a>
    </nav>
    <main>
      <SectionCard id="appearance" class="settings-page__section">
        <ThemeSettings :theme="theme" :t="t" @update:theme="$emit('update:theme', $event)" />
        <LanguageSettings :model-value="locale" :t="t" @update:model-value="$emit('update:locale', $event)" />
        <FormSection :title="t('settings.path_mode')" :description="t('settings.path_mode_desc')">
          <label for="path-mode">{{ t('settings.path_mode') }}</label>
          <select id="path-mode" data-action="path-mode" :value="pathMode" @change="$emit('update:path-mode', $event.target.value)">
            <option value="auto">{{ t('settings.path_mode_auto') }}</option>
            <option value="shim">{{ t('settings.path_mode_shim') }}</option>
            <option value="direct">{{ t('settings.path_mode_direct') }}</option>
          </select>
        </FormSection>
        <FormSection :title="t('migration.mode')" :description="t('migration.desc')">
          <label for="migration-mode">{{ t('migration.mode') }}</label>
          <select id="migration-mode" data-action="migration-mode" :value="migrationMode" @change="$emit('update:migration-mode', $event.target.value)">
            <option value="junction">{{ t('migration.junction') }}</option>
            <option value="move">{{ t('migration.move') }}</option>
          </select>
        </FormSection>
      </SectionCard>

      <SectionCard id="network" class="settings-page__section">
        <MirrorSettings :config="mirrorConfig" :results="mirrorTestResults" :locale="locale" :t="t" @update="(...a) => $emit('mirror-update', ...a)" @test="(...a) => $emit('mirror-test', ...a)" @remove="$emit('mirror-remove', $event)" @add="(...a) => $emit('mirror-add', ...a)" @save="$emit('mirror-save')" />
        <ProxySettings :model-value="proxy" :result="proxyTestResult" :probes="proxyProbes" :t="t" @update:model-value="$emit('update:proxy', $event)" @test="$emit('proxy-test')" @save="$emit('proxy-save')" @probe="$emit('proxy-probe')" @apply-probe="$emit('apply-proxy-probe', $event)" />
      </SectionCard>

      <SectionCard id="custom-tools" class="settings-page__section">
        <template #title>{{ t('custom_tools.title') }}</template>

        <!-- Add form -->
        <div class="ct-section">
          <h3 class="ct-section__title">{{ t('custom_tools.add') }}</h3>
          <div class="ct-grid">
            <label for="ct-name">{{ t('custom_tools.name') }} <span class="required">*</span></label>
            <input id="ct-name" v-model="customToolForm.name" />

            <label for="ct-display-name">{{ t('custom_tools.display_name') }}</label>
            <input id="ct-display-name" v-model="customToolForm.displayName" />

            <label for="ct-url">{{ t('custom_tools.source_url') }} <span class="required">*</span></label>
            <input id="ct-url" v-model="customToolForm.sourceURL" />

            <label for="ct-type">{{ t('custom_tools.source_type') }}</label>
            <select id="ct-type" v-model="customToolForm.sourceType">
              <option value="github">GitHub</option>
              <option value="html">HTML</option>
              <option value="npm">npm</option>
            </select>

            <label for="ct-pattern">{{ t('custom_tools.pattern') }}</label>
            <input id="ct-pattern" v-model="customToolForm.pattern" :placeholder="t('custom_tools.pattern_placeholder')" />

            <label for="ct-shim">{{ t('custom_tools.shim') }}</label>
            <input id="ct-shim" v-model="customToolForm.shim" />

            <label for="ct-envvar">{{ t('custom_tools.env_var') }}</label>
            <input id="ct-envvar" v-model="customToolForm.envVar" />
          </div>
          <div class="ct-actions">
            <button class="xdev-btn xdev-btn--secondary" :disabled="customToolSubmitting" @click="$emit('preview-custom-tool', { ...customToolForm })">
              {{ t('custom_tools.preview') }}
            </button>
            <button class="xdev-btn xdev-btn--primary" :disabled="!customToolForm.name || !customToolForm.sourceURL || customToolSubmitting" @click="$emit('add-custom-tool', { ...customToolForm })">
              {{ t('custom_tools.add') }}
            </button>
          </div>
        </div>

        <!-- Preview results -->
        <div v-if="customToolPreview && customToolPreview.length > 0" class="ct-preview">
          <h3 class="ct-section__title">{{ t('custom_tools.preview_results') }} ({{ customToolPreview.length }})</h3>
          <ul class="ct-preview__list">
            <li v-for="v in customToolPreview" :key="v" class="ct-preview__item">{{ v }}</li>
          </ul>
        </div>

        <!-- Added tools list -->
        <div class="ct-list">
          <h3 class="ct-section__title">{{ t('custom_tools.title') }}</h3>
          <div v-if="customTools.length === 0" class="ct-empty">{{ t('custom_tools.empty') }}</div>
          <div v-for="tool in customTools" :key="tool.name" class="ct-item">
            <div class="ct-item__info">
              <strong class="ct-item__name">{{ tool.displayName || tool.name }}</strong>
              <span class="ct-item__meta">{{ tool.name }}</span>
              <span class="ct-item__meta">{{ tool.sourceUrl }}</span>
              <span class="ct-item__meta ct-item__type">{{ tool.sourceType }}</span>
            </div>
            <button class="xdev-btn xdev-btn--danger ct-item__remove" @click="$emit('remove-custom-tool', tool.name)">
              {{ t('custom_tools.remove') }}
            </button>
          </div>
        </div>
      </SectionCard>

      <SectionCard id="discovery" class="settings-page__section">
        <FormSection :title="t('updates.sniff_strategy')">
          <label for="sniff-strategy">{{ t('updates.sniff_strategy') }}</label>
          <select id="sniff-strategy" :value="sniffConfig.strategy" @change="$emit('save-sniff-strategy', $event.target.value)">
            <option value="interval">{{ t('updates.strategy_interval') }}</option>
            <option value="startup">{{ t('updates.strategy_startup') }}</option>
            <option value="service">{{ t('updates.strategy_service') }}</option>
            <option value="manual">{{ t('updates.strategy_manual') }}</option>
          </select>

          <label for="sniff-interval">{{ t('updates.interval') }}</label>
          <input id="sniff-interval" :value="sniffConfig.interval" readonly />

          <label>
            <input type="checkbox" :checked="sniffConfig.autoArchiveFill" disabled />
            {{ t('updates.auto_archive_fill') }}
          </label>

          <template #actions>
            <n-space>
              <n-button size="small" secondary @click="$emit('refresh-sniff-status')">{{ t('updates.service_status') }}</n-button>
              <n-button size="small" type="primary" @click="$emit('install-sniff-service', '0 3 * * *')">{{ t('updates.service_install') }}</n-button>
              <n-button size="small" type="error" @click="$emit('uninstall-sniff-service')">{{ t('updates.service_uninstall') }}</n-button>
            </n-space>
          </template>
        </FormSection>
        <div v-if="serviceStatus" class="sniff-status">
          <StatusBadge :tone="serviceStatus.installed ? 'success' : 'warning'">
            {{ serviceStatus.installed ? t('updates.service_status_installed') : t('updates.service_status_not_installed') }}
          </StatusBadge>
          <span v-if="serviceStatus.schedule" class="sniff-status__detail">{{ serviceStatus.schedule }}</span>
          <span v-if="serviceStatus.lastRun" class="sniff-status__detail">{{ t('updates.last_sniff') }}: {{ serviceStatus.lastRun }}</span>
          <span v-if="serviceStatus.nextRun" class="sniff-status__detail">{{ t('updates.next_run') }}: {{ serviceStatus.nextRun }}</span>
          <span v-if="serviceStatus.error" class="sniff-status__error">{{ serviceStatus.error }}</span>
        </div>
      </SectionCard>

      <SectionCard id="maintenance" class="settings-page__section">
        <FormSection :title="t('settings.profile')" :description="t('settings.profile_desc')">
          <StatusBadge v-if="importResult" :tone="importResult.success ? 'success' : 'error'">{{ importResult.message }}</StatusBadge>
          <template #actions>
            <button class="xdev-btn xdev-btn--secondary" data-action="profile-export" @click="$emit('profile-export')">{{ t('settings.profile_export') }}</button>
            <button class="xdev-btn xdev-btn--secondary" data-action="profile-import" @click="$emit('profile-import')">{{ t('settings.profile_import') }}</button>
          </template>
        </FormSection>
        <FormSection :title="t('settings.self_update')">
          <StatusBadge v-if="updateInfo" :tone="updateInfo.hasUpdate ? 'warning' : 'success'">
            {{ updateInfo.hasUpdate ? t('update_available', updateInfo) : t('up_to_date') }}
          </StatusBadge>
          <template #actions>
            <button class="xdev-btn xdev-btn--primary" data-action="check-update" @click="$emit('check-update')">{{ t('btn.check_update') }}</button>
          </template>
        </FormSection>
        <FormSection :title="t('settings.diagnostics')">
          <AsyncState :loading="false" :error="null">
            <ResponsiveDataTable :data="diagResults" :columns="diagColumns" />
          </AsyncState>
          <template #actions>
            <button class="xdev-btn xdev-btn--secondary" data-action="run-diagnostics" @click="$emit('run-diagnostics')">{{ t('settings.diagnostics') }}</button>
          </template>
        </FormSection>
        <CacheSettings :breakdown="cacheBreakdown" :retention="archiveRetention" :t="t" @clean="$emit('cache-clean', $event)" @update:retention="v => $emit('update:archive-retention', v)" />
      </SectionCard>
    </main>
  </div>
</template>

<script setup>
import { computed, h, onMounted, ref, reactive } from 'vue';
import {
  NButton, NSpace,
  useMessage, useDialog,
} from 'naive-ui';
import SectionCard from '../components/SectionCard.vue';
import FormSection from '../components/FormSection.vue';
import AsyncState from '../components/AsyncState.vue';
import ResponsiveDataTable from '../components/ResponsiveDataTable.vue';
import StatusBadge from '../components/StatusBadge.vue';
import ThemeSettings from '../components/settings/ThemeSettings.vue';
import LanguageSettings from '../components/settings/LanguageSettings.vue';
import MirrorSettings from '../components/settings/MirrorSettings.vue';
import ProxySettings from '../components/settings/ProxySettings.vue';
import CacheSettings from '../components/settings/CacheSettings.vue';

const p = defineProps({
  t: { type: Function, required: true },
  theme: { type: String, default: 'dark' },
  locale: String,
  pathMode: String,
  migrationMode: String,
  mirrorConfig: Object,
  mirrorTestResults: Object,
  proxy: { type: Object, required: true },
  proxyTestResult: Object,
  proxyProbes: { type: Array, default: () => [] },
  importResult: Object,
  updateInfo: Object,
  diagResults: { type: Array, default: () => [] },
  cacheBreakdown: Object,
  archiveRetention: { type: String, default: 'immediate' },
  sniffConfig: { type: Object, default: () => ({}) },
  serviceStatus: { type: Object, default: null },
  customTools: { type: Array, default: () => [] },
  customToolSubmitting: { type: Boolean, default: false },
  customToolPreview: { type: Array, default: () => [] },
});

defineEmits([
  'update:theme', 'update:locale', 'update:path-mode', 'update:migration-mode',
  'mirror-update', 'mirror-test', 'mirror-remove', 'mirror-add', 'mirror-save',
  'update:proxy', 'proxy-test', 'proxy-save', 'proxy-probe', 'apply-proxy-probe',
  'profile-export', 'profile-import', 'check-update', 'run-diagnostics', 'cache-clean', 'update:archive-retention',
  'save-sniff-strategy', 'install-sniff-service', 'uninstall-sniff-service', 'refresh-sniff-status',
  'preview-custom-tool', 'add-custom-tool', 'remove-custom-tool',
]);

const t = p.t;

const customToolForm = reactive({
  name: '',
  displayName: '',
  sourceURL: '',
  sourceType: 'github',
  pattern: '',
  shim: '',
  envVar: '',
});

const noop = () => {};
const messageFallback = { error: noop, success: noop, warning: noop, info: noop, loading: () => noop };
const dialogFallback = { warning: noop, error: noop, info: noop };
const message = (() => { try { return useMessage(); } catch { return messageFallback; } })();
const dialog = (() => { try { return useDialog(); } catch { return dialogFallback; } })();

const diagColumns = computed(() => [
  { title: p.t('diag.check'), key: 'check', contract: 'identity' },
  { title: p.t('diag.path'), key: 'path', contract: 'actions' },
  { title: p.t('diag.status'), key: 'ok', contract: 'status', render: r => h(StatusBadge, { tone: r.ok ? 'success' : 'error' }, () => p.t(r.ok ? 'diag.ok' : 'diag.fail')) },
]);
</script>

<style scoped>
.settings-page { width: 100%; max-width: var(--xdev-content-max-width); display: grid; grid-template-columns: 160px minmax(0, 1fr); gap: clamp(var(--xdev-space-5), 3vw, var(--xdev-space-8)); align-items: start; }
.settings-page nav { position: sticky; top: var(--xdev-space-4); align-self: start; display: grid; gap: var(--xdev-space-1); padding: var(--xdev-space-2) 0; }
.settings-page nav a { padding: var(--xdev-space-2) var(--xdev-space-3); border-left: 2px solid transparent; border-radius: 0 var(--xdev-radius-sm) var(--xdev-radius-sm) 0; color: var(--xdev-text-secondary); text-decoration: none; transition: color var(--xdev-motion-fast), background var(--xdev-motion-fast), border-color var(--xdev-motion-fast); }
.settings-page nav a:hover { color: var(--xdev-accent); background: var(--xdev-accent-soft); border-left-color: var(--xdev-accent); }
.settings-page main { display: grid; gap: var(--xdev-section-gap); min-width: 0; }
.settings-page__section { min-width: 0; }
.sniff-status { display: flex; align-items: center; gap: var(--xdev-space-3); flex-wrap: wrap; margin-top: var(--xdev-space-3); }
.sniff-status__detail { color: var(--xdev-text-muted); font-size: var(--xdev-font-size-sm); }
.sniff-status__error { color: var(--xdev-error); font-size: var(--xdev-font-size-sm); }
:deep(.xdev-btn), :deep(select), :deep(input:not([type="checkbox"])) { min-height: 34px; border-radius: var(--xdev-radius-sm); font-size: var(--xdev-font-size-md); }
:deep(select), :deep(input:not([type="checkbox"])) { width: 100%; max-width: 480px; padding: 0 var(--xdev-space-3); border: 1px solid var(--xdev-border-subtle); background: var(--xdev-bg-panel); color: var(--xdev-text-primary); }
:deep(select:focus), :deep(input:not([type="checkbox"]):focus) { border-color: var(--xdev-accent); }
:deep(.settings-field) { display: grid; gap: var(--xdev-space-2); max-width: 620px; color: var(--xdev-text-secondary); font-size: var(--xdev-font-size-sm); }
:deep(.settings-check) { display: flex; align-items: center; gap: var(--xdev-space-2); color: var(--xdev-text-secondary); }
:deep(.settings-actions) { max-width: 100%; }
:deep(.settings-actions button) { white-space: normal; }
input[type="checkbox"] { min-height: auto; }

/* Custom tools section */
.ct-section { padding-bottom: var(--xdev-space-5); border-bottom: 1px solid var(--xdev-border-subtle); }
.ct-section__title { margin: 0 0 var(--xdev-space-3); font-size: var(--xdev-font-size-md); color: var(--xdev-text-primary); font-weight: 600; }
.ct-grid { display: grid; grid-template-columns: 100px minmax(0, 1fr); gap: var(--xdev-space-3); align-items: center; max-width: 620px; }
.ct-grid label { color: var(--xdev-text-muted); font-size: var(--xdev-font-size-sm); white-space: nowrap; }
.ct-grid label .required { color: var(--xdev-error); margin-left: 2px; }
.ct-grid input,
.ct-grid select { min-height: 34px; padding: 0 var(--xdev-space-3); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-sm); background: var(--xdev-bg-panel); color: var(--xdev-text-primary); font-size: var(--xdev-font-size-md); width: 100%; }
.ct-grid input:focus,
.ct-grid select:focus { border-color: var(--xdev-accent); outline: none; }
.ct-actions { display: flex; gap: var(--xdev-space-3); margin-top: var(--xdev-space-4); }
.ct-actions button { min-height: 34px; padding: 0 var(--xdev-space-4); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-sm); background: var(--xdev-bg-elevated); color: var(--xdev-text-primary); cursor: pointer; font-size: var(--xdev-font-size-md); transition: border-color var(--xdev-motion-fast), color var(--xdev-motion-fast); white-space: nowrap; }
.ct-actions button:hover:not(:disabled) { border-color: var(--xdev-accent); color: var(--xdev-accent); }
.ct-actions button:disabled { opacity: 0.5; cursor: not-allowed; }
.ct-preview { padding: var(--xdev-space-4) 0; border-bottom: 1px solid var(--xdev-border-subtle); }
.ct-preview__list { display: flex; flex-wrap: wrap; gap: var(--xdev-space-2); margin: var(--xdev-space-2) 0 0; padding: 0; list-style: none; }
.ct-preview__item { display: inline-block; padding: var(--xdev-space-1) var(--xdev-space-3); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-sm); background: var(--xdev-bg-panel); color: var(--xdev-text-primary); font-size: var(--xdev-font-size-sm); font-family: monospace; }
.ct-empty { padding: var(--xdev-space-4) 0; color: var(--xdev-text-muted); font-size: var(--xdev-font-size-sm); }
.ct-item { display: flex; align-items: center; justify-content: space-between; gap: var(--xdev-space-3); padding: var(--xdev-space-3) 0; border-bottom: 1px solid var(--xdev-border-subtle); }
.ct-item:last-child { border-bottom: none; }
.ct-item__info { display: flex; flex-wrap: wrap; align-items: baseline; gap: var(--xdev-space-2); min-width: 0; }
.ct-item__name { color: var(--xdev-text-primary); font-size: var(--xdev-font-size-md); }
.ct-item__meta { color: var(--xdev-text-muted); font-size: var(--xdev-font-size-sm); }
.ct-item__type { text-transform: uppercase; }
.ct-item__remove { min-height: 34px; padding: 0 var(--xdev-space-3); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-sm); background: var(--xdev-bg-elevated); color: var(--xdev-error); cursor: pointer; font-size: var(--xdev-font-size-md); white-space: nowrap; transition: border-color var(--xdev-motion-fast); flex-shrink: 0; }
.ct-item__remove:hover { border-color: var(--xdev-error); }

@media(max-width:760px) {
  .settings-page { width: 100%; gap: var(--xdev-space-4); grid-template-columns: 1fr; }
  .settings-page nav { position: sticky; top: 0; z-index: 2; display: flex; overflow-x: auto; padding: var(--xdev-space-2); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-md); background: color-mix(in srgb, var(--xdev-bg-panel) 94%, transparent); box-shadow: var(--xdev-shadow-sm); }
  .settings-page nav a { white-space: nowrap; }
}
</style>
