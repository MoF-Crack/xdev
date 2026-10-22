<template>
  <div class="env-page">
    <SectionCard>
      <FormSection :title="t('nav.env_vars')">
        <div class="toolbar">
          <label for="env-level">{{ t('env.level') }}</label>
          <select id="env-level" data-action="all-level" :value="allLevel" @change="$emit('update:all-level', $event.target.value)">
            <option value="user">{{ t('env.level_user') }}</option><option value="machine">{{ t('env.level_machine') }}</option>
          </select>
          <button class="xdev-btn xdev-btn--secondary" data-action="validate" @click="$emit('validate')">{{ t('env.detect_invalid') }}</button>
          <button class="xdev-btn xdev-btn--danger" data-action="clean-all-invalid" @click="$emit('clean-all-invalid')">{{ t('env.clean_all_invalid') }}</button>
          <button class="xdev-btn xdev-btn--primary" data-action="new-env" @click="$emit('new')">{{ t('btn.new') }}</button>
          <StatusBadge v-if="validationStats" tone="info">{{ t('env.validation_stats', validationStats) }}</StatusBadge>
        </div>

        <div v-if="allLevel === 'machine'" class="admin-warning">{{ t('env.machine_hint') }}</div>

        <AsyncState :empty="!displayRows.length" retry-label="">
          <div v-if="displayRows.length" class="env-table-wrap">
            <table>
              <thead><tr><th>{{ t('env.var_name') }}</th><th>{{ t('env.var_value') }}</th><th>{{ t('env.status') }}</th><th>{{ t('table.action') }}</th></tr></thead>
              <tbody>
                <tr v-for="row in displayRows" :key="rowKey(row)" :data-row="row.name" :title="row.invalidReason || undefined">
                  <td>{{ row.name }}</td>
                  <td><code :title="row.value">{{ row.value }}</code></td>
                  <td><span class="status-dot" :class="`status-dot--${tone(row)}`" :title="statusTooltip(row)" :aria-label="statusTooltip(row)" role="img"></span></td>
                  <td><div class="actions"><button class="xdev-btn xdev-btn--secondary" data-action="edit-env" @click.stop="$emit('edit', row)">{{ t('btn.edit') }}</button><button class="xdev-btn xdev-btn--danger" data-action="delete-env" @click.stop="request('delete', row)">{{ t('btn.delete') }}</button><button class="xdev-btn xdev-btn--secondary" v-if="canClean(row)" data-action="clean-env" @click.stop="request('clean', row)">{{ t('btn.clean') }}</button></div></td>
                </tr>
              </tbody>
            </table>
            <div class="env-cards">
              <article v-for="row in displayRows" :key="rowKey(row)" class="env-card" :data-row="row.name" :title="row.invalidReason || undefined">
                <strong>{{ row.name }}</strong><code :title="row.value">{{ row.value }}</code>
                <div class="card-status"><span class="status-dot" :class="`status-dot--${tone(row)}`" :title="statusTooltip(row)" :aria-label="statusTooltip(row)" role="img"></span></div>
                <div class="actions"><button class="xdev-btn xdev-btn--secondary" data-action="edit-env" @click="$emit('edit', row)">{{ t('btn.edit') }}</button><button class="xdev-btn xdev-btn--danger" data-action="delete-env" @click="request('delete', row)">{{ t('btn.delete') }}</button><button class="xdev-btn xdev-btn--secondary" v-if="canClean(row)" data-action="clean-env" @click="request('clean', row)">{{ t('btn.clean') }}</button></div>
              </article>
            </div>
          </div>
        </AsyncState>

      </FormSection>
    </SectionCard>

    <SectionCard><FormSection :title="t('migration.title')" :description="t('migration.desc')">
      <div class="toolbar"><span class="migration-mode-label" data-action="migration-mode">{{ t('migration.mode') }} {{ migrationMode === 'move' ? t('migration.move') : t('migration.junction') }}</span><button class="xdev-btn xdev-btn--secondary" data-action="migration-scan" :disabled="migrationBusy" @click="$emit('migration-scan')">{{ t('migration.scan_local') }}</button><button class="xdev-btn xdev-btn--secondary" data-action="migration-all" :disabled="!migrationTools.length || migrationBusy" @click="$emit('migration-all')">{{ t('migration.migrate_all') }}</button></div>
      <article v-for="tool in migrationTools" :key="tool.name" class="migration-row"><strong>{{ tool.name }}</strong><span>{{ tool.version || t('status.unknown') }}</span><code>{{ tool.path }}</code><button class="xdev-btn xdev-btn--secondary" data-action="migration-single" :disabled="migrationBusy" @click="$emit('migration-single', tool)">{{ t('btn.migrate') }}</button></article>
      <div v-for="result in migrationResults" :key="result.name"><StatusBadge :tone="result.status === 'success' ? 'success' : result.status === 'skipped' ? 'warning' : 'error'">● {{ t(`migration.status_${result.status}`) }}</StatusBadge> {{ result.name }} {{ result.error || result.reason }}</div>
    </FormSection></SectionCard>

    <ConfirmAction v-if="pending" :title="t(pending.action === 'delete' ? 'btn.delete' : 'env.confirm_clean')" :description="pending.row.name" :confirm-label="t('btn.confirm_save')" :cancel-label="t('btn.cancel')" @confirm="confirm" @cancel="pending = null"><template #trigger><button ref="confirmTrigger" class="hidden-trigger">Confirm</button></template></ConfirmAction>
  </div>
</template>

<script setup>
import { computed, nextTick, ref } from 'vue';
import AsyncState from '../components/AsyncState.vue';
import ConfirmAction from '../components/ConfirmAction.vue';
import FormSection from '../components/FormSection.vue';
import SectionCard from '../components/SectionCard.vue';
import StatusBadge from '../components/StatusBadge.vue';
const props = defineProps({ t: { type: Function, required: true }, allEnvVars: { type: Array, default: () => [] }, allLevel: String, validationStats: Object, migrationMode: String, migrationTools: { type: Array, default: () => [] }, migrationResults: { type: Array, default: () => [] },
  // 第七轮审计 F-40：迁移进行中标志。此前 AppContent 的 migratingAll/migratingSingle
  // 只被写、从不被读（模板零引用），按钮不随进行中状态禁用，快速双击会让同一工具
  // 被两个并发迁移流程处理（Go 侧也无工具锁）。
  migrationBusy: { type: Boolean, default: false } });
const emit = defineEmits(['update:all-level', 'validate', 'new', 'edit', 'delete', 'clean', 'clean-all-invalid', 'migration-scan', 'migration-single', 'migration-all']);
const displayRows = computed(() => props.allEnvVars);
const rowKey = (row) => `${row.toolName || ''}|${row.level}|${row.name}`;
const status = (row) => props.t(`env.${row.validationStatus || (row.isValid ? 'valid' : 'invalid')}`);
const tone = (row) => ({ valid: 'success', partial: 'warning', invalid: 'error' }[row.validationStatus] || (row.isValid ? 'success' : 'error'));
const statusTooltip = (row) => row.invalidReason ? `${status(row)}: ${row.invalidReason}` : status(row);
const canClean = (row) => ['partial', 'invalid'].includes(row.validationStatus) || (!row.isValid && !!row.invalidReason);
const pending = ref(null); const confirmTrigger = ref(null);
async function request(action, row) { pending.value = { action, row }; await nextTick(); confirmTrigger.value?.click(); }
function confirm() { emit(pending.value.action, pending.value.row); pending.value = null; }
</script>

<style scoped>
.env-page{width:min(var(--xdev-table-max-width),100%);margin:0 auto;display:grid;gap:var(--xdev-section-gap)}
.toolbar,.actions{display:flex;gap:var(--xdev-space-2);align-items:center;flex-wrap:wrap}
.migration-mode-label{color:var(--xdev-text-secondary);font-size:13px}
.env-table-wrap{max-width:100%;overflow:auto}
table{width:100%;min-width:720px;table-layout:fixed;border-collapse:collapse}
th,td{padding:6px var(--xdev-space-3);text-align:left;border-bottom:1px solid var(--xdev-border-subtle)}
th:nth-child(1),td:nth-child(1){width:20%}
th:nth-child(2),td:nth-child(2){width:48%}
th:nth-child(3),td:nth-child(3){width:8%}
th:nth-child(4),td:nth-child(4){width:24%;min-width:132px;white-space:nowrap}
.env-table-wrap td:nth-child(2){max-width:40vw;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.env-table-wrap td:nth-child(2) code{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.env-table-wrap td:nth-child(4) .actions{min-width:132px;white-space:nowrap}
.env-table-wrap td:nth-child(4) .actions > .xdev-btn{white-space:nowrap}
.env-table-wrap tbody tr:hover{background:var(--xdev-bg-hover)}
.status-dot{display:inline-block;width:12px;height:12px;border-radius:50%;flex-shrink:0;vertical-align:middle;cursor:help}
.status-dot--success{background:var(--xdev-success)}
.status-dot--warning{background:var(--xdev-warning)}
.status-dot--error{background:var(--xdev-error)}
.env-cards{display:none}
.env-form{display:grid;grid-template-columns:repeat(4,minmax(140px,1fr));gap:var(--xdev-space-3);align-items:end}
.env-form label{display:grid;gap:var(--xdev-space-1)}
.migration-row{display:grid;grid-template-columns:140px 100px minmax(0,1fr) auto;gap:var(--xdev-space-3);align-items:center;padding:var(--xdev-space-3) 0;border-bottom:1px solid var(--xdev-border-subtle)}
.env-card{display:grid;gap:var(--xdev-space-2);padding:var(--xdev-space-4);border:1px solid var(--xdev-border-subtle);border-radius:var(--xdev-radius-md);background:var(--xdev-bg-elevated);box-shadow:var(--xdev-shadow-sm)}
.hidden-trigger{position:fixed;left:-9999px}
.admin-warning{padding:var(--xdev-space-3);background:var(--xdev-warning-bg, #fff3cd);border:1px solid var(--xdev-warning-border, #ffc107);border-radius:var(--xdev-radius-md);color:var(--xdev-warning-text, #856404);font-size:13px}
@media(max-width:700px){table{display:none}.env-cards{display:grid}.env-table-wrap{overflow:visible}}
</style>
