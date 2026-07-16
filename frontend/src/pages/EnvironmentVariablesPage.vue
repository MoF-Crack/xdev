<template>
  <div class="env-page">
    <SectionCard>
      <FormSection :title="t('nav.env_vars')">
        <div class="toolbar">
          <button data-action="scope-dev" :aria-pressed="viewMode === 'dev'" @click="$emit('update:view-mode', 'dev')">{{ t('env.dev_vars') }}</button>
          <button data-action="scope-all" :aria-pressed="viewMode === 'all'" @click="$emit('update:view-mode', 'all')">{{ t('env.all_vars') }}</button>
          <label v-if="viewMode === 'all'" for="env-level">{{ t('env.level') }}</label>
          <select v-if="viewMode === 'all'" id="env-level" data-action="all-level" :value="allLevel" @change="$emit('update:all-level', $event.target.value)">
            <option value="user">{{ t('env.level_user') }}</option><option value="machine">{{ t('env.level_machine') }}</option>
          </select>
          <button data-action="validate" @click="$emit('validate')">{{ t('env.detect_invalid') }}</button>
          <StatusBadge v-if="validationStats" tone="info">{{ t('env.validation_stats', validationStats) }}</StatusBadge>
        </div>

        <AsyncState :empty="!displayRows.length" retry-label="">
          <div v-if="displayRows.length" class="env-table-wrap">
            <table>
              <thead><tr><th>{{ t('env.var_name') }}</th><th>{{ t('env.var_value') }}</th><th>{{ t('env.status') }}</th><th>{{ t('env.level') }}</th><th>{{ t('table.action') }}</th></tr></thead>
              <tbody><tr v-for="row in displayRows" :key="rowKey(row)" :data-row="row.name">
                <td>{{ row.name }}</td><td><code>{{ row.value }}</code></td><td><StatusBadge :tone="tone(row)">● {{ status(row) }}</StatusBadge></td><td>{{ row.level }}</td>
                <td><div class="actions"><button data-action="edit-env" @click.stop="$emit('edit', row)">{{ t('btn.edit') }}</button><button data-action="delete-env" @click.stop="request('delete', row)">{{ t('btn.delete') }}</button><button v-if="canClean(row)" data-action="clean-env" @click.stop="request('clean', row)">{{ t('btn.clean') }}</button></div></td>
              </tr></tbody>
            </table>
            <div class="env-cards">
              <article v-for="row in displayRows" :key="rowKey(row)" class="env-card" :data-row="row.name">
                <strong>{{ row.name }}</strong><code>{{ row.value }}</code><StatusBadge :tone="tone(row)">● {{ status(row) }}</StatusBadge>
                <div class="actions"><button data-action="edit-env" @click="$emit('edit', row)">{{ t('btn.edit') }}</button><button data-action="delete-env" @click="request('delete', row)">{{ t('btn.delete') }}</button><button v-if="canClean(row)" data-action="clean-env" @click="request('clean', row)">{{ t('btn.clean') }}</button></div>
              </article>
            </div>
          </div>
        </AsyncState>

        <div class="env-form">
          <label>{{ t('env.tool_name') }}<input v-model="draft.toolName" aria-label="Tool name" :disabled="!!editing"></label>
          <label>{{ t('env.var_name') }}<input v-model="draft.name" aria-label="Variable name" :disabled="!!editing"></label>
          <label>{{ t('env.var_value') }}<input v-model="draft.value" aria-label="Variable value"></label>
          <button data-action="browse" @click="browse">{{ t('btn.browse') }}</button>
          <label>{{ t('env.level') }}<select v-model="draft.level" aria-label="Level" :disabled="!!editing"><option value="user">{{ t('env.level_user') }}</option><option value="machine">{{ t('env.level_machine') }}</option></select></label>
          <button data-action="save-env" @click="save">{{ t(editing ? 'btn.save' : 'btn.add') }}</button>
          <button v-if="editing" data-action="cancel-edit" @click="$emit('cancel')">{{ t('btn.cancel') }}</button>
        </div>
      </FormSection>
    </SectionCard>

    <SectionCard><FormSection :title="t('migration.title')" :description="t('migration.desc')">
      <div class="toolbar"><button data-action="migration-mode-junction" :aria-pressed="migrationMode === 'junction'" @click="$emit('update:migration-mode', 'junction')">{{ t('migration.junction') }}</button><button data-action="migration-mode-move" :aria-pressed="migrationMode === 'move'" @click="$emit('update:migration-mode', 'move')">{{ t('migration.move') }}</button><button data-action="migration-scan" @click="$emit('migration-scan')">{{ t('migration.scan_local') }}</button><button data-action="migration-all" :disabled="!migrationTools.length" @click="$emit('migration-all', migrationMode)">{{ t('migration.migrate_all') }}</button></div>
      <article v-for="tool in migrationTools" :key="tool.name" class="migration-row"><strong>{{ tool.name }}</strong><span>{{ tool.version || t('status.unknown') }}</span><code>{{ tool.path }}</code><button data-action="migration-single" @click="$emit('migration-single', tool)">{{ t('btn.migrate') }}</button></article>
      <div v-for="result in migrationResults" :key="result.name"><StatusBadge :tone="result.status === 'success' ? 'success' : result.status === 'skipped' ? 'warning' : 'error'">● {{ t(`migration.status_${result.status}`) }}</StatusBadge> {{ result.name }} {{ result.error || result.reason }}</div>
    </FormSection></SectionCard>

    <ConfirmAction v-if="pending" :title="t(pending.action === 'delete' ? 'btn.delete' : 'env.confirm_clean')" :description="pending.row.name" :confirm-label="t('btn.confirm_save')" :cancel-label="t('btn.cancel')" @confirm="confirm" @cancel="pending = null"><template #trigger><button ref="confirmTrigger" class="hidden-trigger">Confirm</button></template></ConfirmAction>
  </div>
</template>

<script setup>
import { computed, nextTick, reactive, ref, watch } from 'vue';
import AsyncState from '../components/AsyncState.vue';
import ConfirmAction from '../components/ConfirmAction.vue';
import FormSection from '../components/FormSection.vue';
import SectionCard from '../components/SectionCard.vue';
import StatusBadge from '../components/StatusBadge.vue';
const props = defineProps({ t: { type: Function, required: true }, envVars: { type: Array, default: () => [] }, allEnvVars: { type: Array, default: () => [] }, viewMode: String, allLevel: String, validationStats: Object, editing: Object, browseDirectory: { type: Function, required: true }, migrationMode: String, migrationTools: { type: Array, default: () => [] }, migrationResults: { type: Array, default: () => [] } });
const emit = defineEmits(['update:view-mode', 'update:all-level', 'validate', 'edit', 'save', 'cancel', 'delete', 'clean', 'update:migration-mode', 'migration-scan', 'migration-single', 'migration-all']);
const draft = reactive({ toolName: '', name: '', value: '', level: 'user' });
watch(() => props.editing, (row) => Object.assign(draft, row ? { toolName: row.toolName || '', name: row.name, value: row.value, level: row.level || 'user' } : { toolName: '', name: '', value: '', level: 'user' }), { immediate: true });
const displayRows = computed(() => props.viewMode === 'all' ? props.allEnvVars : props.envVars);
const rowKey = (row) => `${row.toolName || ''}|${row.level}|${row.name}`;
const status = (row) => props.t(`env.${row.validationStatus || (row.isValid ? 'valid' : 'invalid')}`);
const tone = (row) => ({ valid: 'success', partial: 'warning', invalid: 'error' }[row.validationStatus] || (row.isValid ? 'success' : 'error'));
const canClean = (row) => ['partial', 'invalid'].includes(row.validationStatus) || (!row.isValid && !!row.invalidReason);
async function browse() { const dir = await props.browseDirectory(); if (dir) draft.value = dir; }
function save() { emit('save', { ...draft, original: props.editing ? { toolName: props.editing.toolName || '', name: props.editing.name, level: props.editing.level || 'user' } : null }); }
const pending = ref(null); const confirmTrigger = ref(null);
async function request(action, row) { pending.value = { action, row }; await nextTick(); confirmTrigger.value?.click(); }
function confirm() { emit(pending.value.action, pending.value.row); pending.value = null; }
</script>

<style scoped>
.env-page{display:grid;gap:20px}.toolbar,.actions{display:flex;gap:8px;align-items:center;flex-wrap:wrap}.env-table-wrap{max-width:100%;overflow:auto}table{width:100%;border-collapse:collapse}th,td{padding:10px;text-align:left;border-bottom:1px solid var(--xdev-border-subtle)}.env-cards{display:none}.env-form{display:grid;grid-template-columns:repeat(4,minmax(120px,1fr));gap:10px;align-items:end}.env-form label{display:grid;gap:4px}.migration-row{display:grid;grid-template-columns:120px 100px 1fr auto;gap:10px;align-items:center}.env-card{display:grid;gap:8px;padding:12px;border:1px solid var(--xdev-border-subtle);border-radius:var(--xdev-radius-md)}.hidden-trigger{position:fixed;left:-9999px}@media(max-width:700px){table{display:none}.env-cards{display:grid;gap:10px}.env-form,.migration-row{grid-template-columns:1fr}.env-form input,.env-form select,.env-form button{width:100%;min-width:0}}
</style>
