<template>
  <FormSection id="network" :title="t('settings.mirror_config')" :description="t('mirror.connectivity_description')">
    <div v-for="(value, name) in config" :key="name" class="mirror" :data-mirror="name">
      <div class="mirror__heading">
        <StatusBadge :tone="role(name, value) === 'official' ? 'info' : 'neutral'">{{ t(`mirror.role_${role(name, value)}`) }}</StatusBadge>
        <strong>{{ name }}</strong>
      </div>
      <div class="mirror__row">
        <label :for="`mirror-${name}`">{{ t('mirror.url') }}</label>
        <input :id="`mirror-${name}`" :value="value" @input="$emit('update', name, $event.target.value)">
        <div class="mirror__btns">
          <button class="xdev-btn xdev-btn--secondary" data-action="mirror-test" type="button" @click="$emit('test', name, value)">{{ t('btn.test') }}</button>
          <ConfirmAction :title="t('mirror.remove_title')" :description="value" :confirm-label="t('btn.remove')" :cancel-label="t('btn.cancel')" @confirm="$emit('remove', name)">
            <template #trigger><button class="xdev-btn xdev-btn--danger" data-action="mirror-remove" type="button">{{ t('btn.remove') }}</button></template>
          </ConfirmAction>
        </div>
      </div>
      <div v-if="results[name] && results[name].url === value" class="result">
        <StatusBadge :tone="results[name].success ? 'success' : 'error'">{{ t(results[name].success ? 'mirror.connectivity_passed' : 'mirror.connectivity_failed') }}</StatusBadge>
        <span v-if="results[name].latency != null">{{ results[name].latency }} ms</span>
        <time v-if="results[name].lastChecked">{{ t('mirror.last_checked') }}: {{ formatTime(results[name].lastChecked) }}</time>
      </div>
    </div>
    <div class="add">
      <div class="add__row">
        <label for="new-mirror-tool">{{ t('settings.mirror_tool_placeholder') }}</label>
        <input id="new-mirror-tool" v-model="tool" class="add__tool">
        <label for="new-mirror-url">{{ t('mirror.url') }}</label>
        <input id="new-mirror-url" v-model="url" class="add__url">
        <button class="xdev-btn xdev-btn--secondary" type="button" data-action="mirror-add" :disabled="!tool || !url" @click="add">{{ t('btn.add') }}</button>
      </div>
      <p v-if="error" role="alert">{{ t(error) }}</p>
    </div>
    <template #actions><button class="xdev-btn xdev-btn--secondary" type="button" data-action="mirror-save" @click="save">{{ t('settings.save_mirrors') }}</button></template>
  </FormSection>
</template>
<script setup>
import { ref } from 'vue';
import FormSection from '../FormSection.vue';
import StatusBadge from '../StatusBadge.vue';
import ConfirmAction from '../ConfirmAction.vue';
const props = defineProps({ config: { type: Object, default: () => ({}) }, results: { type: Object, default: () => ({}) }, locale: { type: String, default: 'en' }, t: { type: Function, required: true } });
const emit = defineEmits(['update', 'test', 'remove', 'add', 'save']);
const tool = ref(''); const url = ref(''); const error = ref('');
const valid = value => { try { return new URL(value).protocol === 'https:'; } catch { return false; } };
function add() { if (!valid(url.value)) { error.value = 'mirror.url_https'; return; } error.value = ''; emit('add', tool.value, url.value); tool.value = ''; url.value = ''; }
function save() { if (Object.values(props.config).some(value => !valid(value))) { error.value = 'mirror.url_https'; return; } error.value = ''; emit('save'); }
const role = (name, value) => /nodejs\.org|github\.com|golang\.org|go\.dev/.test(value) ? 'official' : ['node', 'go'].includes(name) ? 'configured' : 'mirror';
const formatTime = value => new Intl.DateTimeFormat(props.locale, { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(value));
</script>
<style scoped>
.mirror { padding: var(--xdev-space-4) 0; border-bottom: 1px solid var(--xdev-border-subtle); }
.mirror__heading { display: flex; align-items: center; gap: var(--xdev-space-2); margin-bottom: var(--xdev-space-3); }
.mirror__heading strong { color: var(--xdev-text-primary); font-size: var(--xdev-font-size-md); }
.mirror__row { display: grid; grid-template-columns: 60px minmax(0, 1fr) auto; gap: var(--xdev-space-3); align-items: center; }
.mirror__row label { color: var(--xdev-text-muted); font-size: var(--xdev-font-size-sm); }
.mirror__row input { min-width: 0; width: 100%; min-height: 34px; padding: 0 var(--xdev-space-3); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-sm); background: var(--xdev-bg-panel); color: var(--xdev-text-primary); font-size: var(--xdev-font-size-md); }
.mirror__row input:focus { border-color: var(--xdev-accent); }
.mirror__btns { display: flex; gap: var(--xdev-space-2); }
.mirror__btns button { min-height: 34px; padding: 0 var(--xdev-space-3); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-sm); background: var(--xdev-bg-elevated); color: var(--xdev-text-primary); cursor: pointer; font-size: var(--xdev-font-size-md); white-space: nowrap; transition: border-color var(--xdev-motion-fast), color var(--xdev-motion-fast); }
.mirror__btns button:hover { border-color: var(--xdev-accent); color: var(--xdev-accent); }
.result { display: flex; align-items: center; gap: var(--xdev-space-3); margin-top: var(--xdev-space-2); padding-left: 72px; min-height: 20px; color: var(--xdev-text-muted); font-size: var(--xdev-font-size-sm); }
.add { padding-top: var(--xdev-space-5); }
.add__row { display: grid; grid-template-columns: 60px 120px 60px minmax(0, 1fr) auto; gap: var(--xdev-space-3); align-items: center; }
.add__row label { color: var(--xdev-text-muted); font-size: var(--xdev-font-size-sm); }
.add__row input { min-width: 0; width: 100%; min-height: 34px; padding: 0 var(--xdev-space-3); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-sm); background: var(--xdev-bg-panel); color: var(--xdev-text-primary); font-size: var(--xdev-font-size-md); }
.add__row input:focus { border-color: var(--xdev-accent); }
.add__row button { min-height: 34px; padding: 0 var(--xdev-space-3); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-sm); background: var(--xdev-bg-elevated); color: var(--xdev-text-primary); cursor: pointer; font-size: var(--xdev-font-size-md); white-space: nowrap; }
.add__row button:hover { border-color: var(--xdev-accent); color: var(--xdev-accent); }
.add p { margin: var(--xdev-space-2) 0 0; color: var(--xdev-error); font-size: var(--xdev-font-size-sm); }
@media (max-width: 700px) {
  .mirror__row { grid-template-columns: 1fr; gap: var(--xdev-space-2); }
  .mirror__btns { justify-content: flex-end; }
  .result { padding-left: 0; }
  .add__row { grid-template-columns: 1fr; gap: var(--xdev-space-2); }
  .add__row label { margin-top: var(--xdev-space-1); }
}
</style>
