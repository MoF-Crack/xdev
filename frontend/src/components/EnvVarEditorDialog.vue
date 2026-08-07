<template>
  <div v-if="show" class="env-modal">
    <div class="env-mask" @click="close" />
    <section ref="dialog" class="env-dialog" role="dialog" aria-modal="true" tabindex="-1" :aria-labelledby="titleId" @keydown="onKeydown">
      <h2 :id="titleId">{{ t(editing ? 'env.edit_title' : 'env.new_title') }}</h2>
      <div class="env-fields">
        <label>{{ t('env.var_name') }}<input ref="nameInput" v-model="draft.name" :disabled="!!editing" /></label>
        <div class="path-field">
          <div class="field-label">{{ t('env.var_value') }}</div>
          <div class="path-list" role="list">
            <div v-for="(path, index) in paths" :key="pathKeys[index]" class="path-entry" :class="{ 'path-entry--invalid': isInvalidPath(path) }" role="listitem">
              <span class="path-status" aria-hidden="true">{{ isInvalidPath(path) ? '✕' : '' }}</span>
              <input v-model="paths[index]" class="path-input xdev-mono" :aria-label="`${t('env.var_value')} ${index + 1}`" />
              <span class="path-error" v-if="isInvalidPath(path)">{{ t('env.path_missing') }}</span>
              <span class="path-error-spacer" v-else></span>
              <button class="path-remove" type="button" :aria-label="t('btn.delete')" @click="removePath(index)">✕</button>
            </div>
          </div>
          <div class="path-actions">
            <button class="xdev-btn xdev-btn--secondary xdev-btn--sm" type="button" @click="addPath">＋ {{ t('env.add_path') }}</button>
            <button class="xdev-btn xdev-btn--secondary xdev-btn--sm" type="button" @click="browse">{{ t('btn.browse') }}</button>
          </div>
        </div>
        <label v-if="!editing">{{ t('env.level') }}<select v-model="draft.level"><option value="user">{{ t('env.level_user') }}</option><option value="machine">{{ t('env.level_machine') }}</option></select></label>
      </div>
      <div v-if="draft.level === 'machine'" class="admin-warning">{{ t('env.machine_hint') }}</div>
      <footer><button class="xdev-btn xdev-btn--secondary" type="button" @click="close">{{ t('btn.cancel') }}</button><button class="xdev-btn xdev-btn--primary" type="button" :disabled="!draft.name.trim()" @click="save">{{ t('btn.save') }}</button></footer>
    </section>
  </div>
</template>

<script setup>
import { nextTick, ref, watch } from 'vue';

const props = defineProps({ show: Boolean, editing: Object, browseDirectory: { type: Function, default: null }, t: { type: Function, required: true }, returnFocusTo: Object });
const emit = defineEmits(['update:show', 'save']);
const dialog = ref(null); const nameInput = ref(null); const titleId = 'env-editor-title';
const draft = ref({ toolName: '', name: '', level: 'user' }); const paths = ref(['']); const pathKeys = ref([]); let nextPathKey = 0; let openingFocus = null;
const pathPattern = /^([A-Za-z]:[\\/]|[\\/]|[A-Za-z])/;

function resetPaths(value) { paths.value = value ? value.split(';') : ['']; pathKeys.value = paths.value.map(() => nextPathKey++); }
watch(() => props.show, async (visible) => {
  if (!visible) return;
  draft.value = props.editing ? { toolName: props.editing.toolName || '', name: props.editing.name || '', level: props.editing.level || 'user' } : { toolName: '', name: '', level: 'user' };
  resetPaths(props.editing?.value || ''); openingFocus = props.returnFocusTo || document.activeElement; await nextTick(); nameInput.value?.focus();
});
watch(() => props.show, (visible, old) => { if (!visible && old) (props.returnFocusTo || openingFocus)?.focus?.(); });
function invalidPathSet() {
  if (!props.editing || !props.editing.pathEntries) return new Set();
  const s = new Set();
  for (const pe of props.editing.pathEntries) {
    if (!pe.exists) s.add(pe.path.toLowerCase());
  }
  return s;
}
function isInvalidPath(path) { const value = path.trim(); return !!value && pathPattern.test(value) && invalidPathSet().has(value.toLowerCase()); }
function addPath() { paths.value.push(''); pathKeys.value.push(nextPathKey++); }
function removePath(index) { paths.value.splice(index, 1); pathKeys.value.splice(index, 1); if (!paths.value.length) addPath(); }
async function browse() {
  const browseDirectory = props.browseDirectory || (() => window.go?.gui?.App?.BrowseDirectory?.(''));
  const value = await browseDirectory();
  if (value) { if (paths.value.length === 1 && !paths.value[0]) paths.value[0] = value; else { paths.value.push(value); pathKeys.value.push(nextPathKey++); } }
}
function close() { emit('update:show', false); }
function save() { emit('save', { ...draft.value, value: paths.value.join(';'), original: props.editing ? { toolName: props.editing.toolName || '', name: props.editing.name, level: props.editing.level || 'user' } : null }); emit('update:show', false); }
function onKeydown(event) { if (event.key === 'Escape') { event.preventDefault(); close(); return; } if (event.key !== 'Tab') return; const items = [...dialog.value.querySelectorAll('button:not(:disabled), input:not(:disabled), textarea:not(:disabled), select:not(:disabled)')]; const first = items[0], last = items.at(-1); if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); } else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); } }
</script>

<style scoped>
.env-modal{position:fixed;left:216px;right:0;top:0;bottom:0;z-index:3000;display:grid;place-items:center}.env-mask{position:absolute;inset:0;background:var(--xdev-overlay)}.env-dialog{position:relative;width:min(620px,calc(100% - 24px));max-height:calc(100vh - 32px);overflow:auto;padding:var(--xdev-space-5);border:1px solid var(--xdev-border-strong);border-radius:var(--xdev-radius-md);background:var(--xdev-bg-panel);color:var(--xdev-text-primary);box-shadow:var(--xdev-shadow-lg)}h2{margin:0 0 var(--xdev-space-5)}.env-fields{display:grid;gap:var(--xdev-space-4)}label,.field-label{display:grid;gap:var(--xdev-space-1)}input,select{width:100%;box-sizing:border-box;background:var(--xdev-bg-elevated);border:1px solid var(--xdev-border-strong);border-radius:var(--xdev-radius-sm);color:var(--xdev-text-primary);padding:9px}.path-list{display:grid;gap:var(--xdev-space-1);margin-top:var(--xdev-space-1);max-height:260px;overflow:auto}.path-entry{display:grid;grid-template-columns:18px minmax(0,1fr) auto 28px;gap:var(--xdev-space-1);align-items:center;padding:2px 4px;border:1px solid transparent;border-radius:var(--xdev-radius-sm)}.path-entry:focus-within{border-color:var(--xdev-accent);background:var(--xdev-accent-soft)}.path-entry--invalid{border-color:var(--xdev-error);background:var(--xdev-error-soft)}.path-entry--invalid input{border-color:var(--xdev-error);color:var(--xdev-error)}.path-status{color:var(--xdev-error);font-weight:700;width:18px;text-align:center;flex-shrink:0}.path-input{min-width:0;width:100%}.path-error{color:var(--xdev-error);font-size:var(--xdev-font-size-xs);white-space:nowrap}.path-error-spacer{width:0}.path-remove{width:28px;height:28px;padding:0;border:0;border-radius:var(--xdev-radius-sm);background:transparent;color:var(--xdev-text-muted);cursor:pointer;flex-shrink:0}.path-remove:hover{background:var(--xdev-error-soft);color:var(--xdev-error)}.path-actions{display:flex;gap:var(--xdev-space-2);margin-top:var(--xdev-space-2)}footer{display:flex;justify-content:flex-end;gap:var(--xdev-space-2);margin-top:var(--xdev-space-5)}.admin-warning{margin-top:var(--xdev-space-4);padding:var(--xdev-space-3);border:1px solid var(--xdev-warning);border-radius:var(--xdev-radius-sm);color:var(--xdev-warning);background:var(--xdev-warning-soft)}@media(max-width:520px){.path-entry{grid-template-columns:18px minmax(0,1fr) auto}.path-error{grid-column:2}.path-remove{grid-column:3;grid-row:1}.env-dialog{padding:var(--xdev-space-4)}}
</style>
