<template>
  <AsyncState :loading="loading" :error="error" @retry="$emit('list')">
    <main class="page">
      <PageHeader :title="t('nav.remote')">
        <template #primary><button class="xdev-btn xdev-btn--secondary" data-action="refresh-remote" @click="$emit('list')">{{t('btn.refresh')}}</button></template>
      </PageHeader>

      <SectionCard :title="t('remote.create')">
        <div class="remote-create-form">
          <label class="remote-form-field">
            <span>{{t('remote.adapter')}}</span>
            <select v-model="adapter"><option value="docker">{{t('remote.adapter_docker')}}</option><option value="ssh">{{t('remote.adapter_ssh')}}</option></select>
          </label>
          <template v-if="adapter==='docker'">
            <label class="remote-form-field">
              <span>{{t('remote.image')}}</span>
              <input v-model.trim="image" data-field="remote-image" :placeholder="t('remote.image_placeholder')">
            </label>
          </template>
          <template v-else>
            <div class="remote-form-host-row">
              <label class="remote-form-field">
                <span>{{t('remote.ssh_host')}}</span>
                <input v-model.trim="host" data-field="remote-host" :placeholder="t('remote.ssh_host_placeholder')">
              </label>
              <label class="remote-form-field remote-form-field--port">
                <span>{{t('remote.ssh_port')}}</span>
                <input v-model.number="port" type="number" min="1" max="65535">
              </label>
            </div>
            <label class="remote-form-field">
              <span>{{t('remote.ssh_user')}}</span>
              <input v-model.trim="user" :placeholder="t('remote.ssh_user_placeholder')">
            </label>
            <label class="remote-form-field">
              <span>{{t('remote.ssh_keypath')}}</span>
              <div class="key-row">
                <input v-model.trim="keyPath" data-field="remote-keypath" :placeholder="t('remote.ssh_keypath_placeholder')">
                <button class="xdev-btn xdev-btn--secondary xdev-btn--sm" type="button" data-action="browse-key" @click="browseKey">{{t('btn.browse')}}</button>
              </div>
            </label>
            <label class="remote-form-field">
              <span>{{t('remote.ssh_password')}}</span>
              <input v-model="password" type="password" autocomplete="off" data-field="remote-password">
            </label>
          </template>
          <div class="remote-form-actions">
            <div v-if="adapter==='ssh'" class="connection-actions">
              <button class="xdev-btn xdev-btn--secondary" type="button" data-action="test-ssh" :disabled="testing" @click="test">{{ testing ? t('remote.testing') : t('remote.test_conn') }}</button>
              <StatusBadge v-if="testResult" :tone="testResult.success ? 'success' : 'error'">{{ testResult.message }}</StatusBadge>
            </div>
            <button class="xdev-btn xdev-btn--primary" data-action="create-remote" :disabled="creating||(adapter==='docker'?!image:!host)" @click="create">{{t('remote.create_btn')}}</button>
          </div>
        </div>
      </SectionCard>

      <SectionCard :title="t('remote.instances')">
        <article v-for="inst in instances" :key="inst.id" :data-instance="inst.id">
          <h3>{{inst.id}} <StatusBadge :tone="inst.status==='running'?'success':'neutral'">{{inst.status}}</StatusBadge></h3>
          <CopyableValue v-for="path in inst.addresses||[]" :key="path" :value="path" :copy-label="t('remote.copy_address')"/>
          <div class="actions">
            <button class="xdev-btn xdev-btn--secondary" v-if="inst.addresses?.[0]" data-action="copy-address" @click="$emit('copy-address',inst.addresses[0])">{{t('remote.copy_address')}}</button>
            <ConfirmAction :title="t('remote.destroy_confirm')" :pending="!!operating[inst.id]?.destroy" @confirm="$emit('destroy',inst.adapter,inst.id)"><template #trigger><button class="xdev-btn xdev-btn--danger" data-action="destroy-remote" :disabled="!!operating[inst.id]?.destroy">{{t('remote.destroy_btn')}}</button></template></ConfirmAction>
          </div>
          <label>{{t('remote.exec_placeholder')}}<input v-model.trim="commands[inst.id]" data-field="remote-command" @keyup.enter="run(inst)"></label>
          <button class="xdev-btn xdev-btn--secondary" data-action="exec-remote" :disabled="!!operating[inst.id]?.exec||!commands[inst.id]" @click="run(inst)">{{t('btn.run')}}</button>
          <CopyableValue v-if="outputs[inst.id]" :value="outputs[inst.id]" :max-length="120"/>
        </article>
        <p v-if="!instances.length">{{t('remote.no_instances')}}</p>
      </SectionCard>
    </main>
  </AsyncState>
</template>

<script setup>
import { reactive, ref } from 'vue';
import AsyncState from '../components/AsyncState.vue';
import ConfirmAction from '../components/ConfirmAction.vue';
import CopyableValue from '../components/CopyableValue.vue';
import PageHeader from '../components/PageHeader.vue';
import SectionCard from '../components/SectionCard.vue';
import StatusBadge from '../components/StatusBadge.vue';

const props = defineProps({
  t: { type: Function, required: true },
  instances: { type: Array, default: () => [] },
  loading: Boolean,
  error: [String, Error],
  creating: Boolean,
  operating: { type: Object, default: () => ({}) },
  outputs: { type: Object, default: () => ({}) },
  browseFile: { type: Function, default: null },
  testSSH: { type: Function, default: null },
});
const emit = defineEmits(['list', 'create', 'copy-address', 'exec', 'destroy']);

const adapter = ref('docker'), image = ref('');
const host = ref(''), port = ref(22), user = ref('root'), keyPath = ref(''), password = ref('');
const commands = reactive({});
const testing = ref(false);
const testResult = ref(null);

const create = () => emit('create', {
  adapter: adapter.value, image: image.value,
  sshHost: host.value, sshPort: port.value, sshUser: user.value,
  sshKeyPath: keyPath.value, sshPassword: password.value,
});

async function browseKey() {
  const fn = props.browseFile || ((title) => window.go?.gui?.App?.BrowseFile?.(title, '*.pem;*.key;*.id_rsa;*.id_ed25519;*.id_ecdsa;*.id_dsa'));
  const picked = await fn(props.t('remote.select_key'));
  if (picked) keyPath.value = picked;
}

async function test() {
  testing.value = true;
  testResult.value = null;
  try {
    const fn = props.testSSH || ((h, p, u, k, pw) => window.go?.gui?.App?.TestSSHConnection?.(h, p, u, k, pw));
    testResult.value = await fn(host.value, port.value || 22, user.value, keyPath.value, password.value);
  } catch (e) {
    testResult.value = { success: false, message: String(e) };
  } finally {
    testing.value = false;
  }
}

const run = inst => commands[inst.id] && emit('exec', inst.adapter, inst.id, commands[inst.id]);
</script>

<style scoped>
.page { width: min(var(--xdev-content-max-width), 100%); margin: 0 auto; display: grid; gap: var(--xdev-section-gap); min-width: 0; }
article { display: grid; gap: var(--xdev-space-3); padding: var(--xdev-space-4); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-md); background: var(--xdev-bg-elevated); overflow: hidden; box-shadow: var(--xdev-shadow-sm); }
article:hover { border-color: var(--xdev-border-strong); box-shadow: var(--xdev-shadow-md); }
.remote-create-form { width: min(100%, 720px); display: grid; gap: var(--xdev-space-4); }
.remote-form-field { display: grid; grid-template-columns: 120px minmax(0, 1fr); align-items: center; gap: var(--xdev-space-3); min-width: 0; color: var(--xdev-text-secondary); font-size: var(--xdev-font-size-sm); }
.remote-form-field > span { white-space: nowrap; }
.remote-form-field input,
.remote-form-field select { width: 100%; min-width: 0; min-height: 34px; box-sizing: border-box; padding: 0 var(--xdev-space-3); border: 1px solid var(--xdev-border-strong); border-radius: var(--xdev-radius-sm); background: var(--xdev-bg-panel); color: var(--xdev-text-primary); font: inherit; font-size: var(--xdev-font-size-md); }
.remote-form-field input:focus,
.remote-form-field select:focus { border-color: var(--xdev-accent); outline: none; }
.remote-form-host-row { display: grid; grid-template-columns: minmax(0, 1fr) 252px; gap: var(--xdev-space-4); min-width: 0; }
.remote-form-field--port input { width: 120px; }
.key-row { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: var(--xdev-space-2); min-width: 0; }
.key-row .xdev-btn { min-height: 34px; }
.remote-form-actions { display: flex; align-items: center; justify-content: space-between; gap: var(--xdev-space-4); min-height: 34px; }
.connection-actions { display: flex; align-items: center; gap: var(--xdev-space-3); flex-wrap: wrap; }
.remote-form-actions > .xdev-btn { min-height: 34px; min-width: 96px; margin-left: auto; }
.actions { display: flex; gap: var(--xdev-space-2); flex-wrap: wrap; align-items: center; }
@media (max-width: 640px) {
  .remote-form-host-row { grid-template-columns: 1fr; }
  .remote-form-field { grid-template-columns: 1fr; align-items: stretch; gap: var(--xdev-space-1); }
  .remote-form-field--port input { width: 100%; }
  .remote-form-actions { flex-direction: column; align-items: stretch; }
  .connection-actions { width: 100%; flex-direction: column; align-items: stretch; }
  .connection-actions > .xdev-btn { width: 100%; }
  .connection-actions > .status-badge { align-self: flex-start; }
  .remote-form-actions > .xdev-btn { width: 100%; }
  .key-row .xdev-btn { min-width: 44px; }
}
</style>
