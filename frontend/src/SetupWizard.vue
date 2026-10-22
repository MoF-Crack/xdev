<template>
  <main class="setup-container">
    <section class="setup-card" aria-labelledby="setup-title">
      <header>
        <h1 id="setup-title" class="setup-title">{{ t('setup.title') }}</h1>
        <p class="setup-subtitle">{{ t('setup.subtitle') }}</p>
      </header>

      <SetupProgress :current="step" :label="t('setup.progress_label')" :steps="progressSteps" />

      <div v-if="step === 1" class="setup-step">
        <fieldset class="drive-fieldset">
          <legend ref="stepOneHeading" tabindex="-1" data-step-heading="1">{{ t('setup.select_drive') }}</legend>
          <p class="setup-hint">{{ t('setup.drive_hint') }}</p>
          <p v-if="loading" role="status">{{ t('setup.loading_drives') }}</p>
          <div v-else-if="loadError">
            <p id="setup-load-error" ref="loadErrorElement" role="alert" tabindex="-1" class="setup-error">{{ loadError }}</p>
            <button type="button" class="xdev-btn xdev-btn--secondary button secondary" data-action="retry-drives" aria-describedby="setup-load-error" @click="loadDrives({ restoreFocus: true })">{{ t('btn.retry') }}</button>
          </div>
          <p v-else-if="!drives.length" ref="emptyDrivesElement" role="status" tabindex="-1" class="setup-empty">{{ t('setup.no_drives') }}</p>
          <div v-else class="drive-list">
            <DriveOption
              v-for="drive in drives" :key="drive.letter" v-model="selectedDrive" :drive="drive"
              :system="drive.letter === 'C:'" :recommended="isRecommended(drive)" :space="driveSpace(drive)"
              :system-label="t('setup.system_drive')" :recommended-label="t('setup.recommended')"
              :system-reason="t('setup.system_drive_reason')" :recommended-reason="t('setup.recommended_reason')"
              @update:model-value="customPath = ''"
            />
          </div>
        </fieldset>

        <div class="custom-path">
          <div>
            <strong>{{ t('setup.custom_path') }}</strong>
            <p v-if="customPath" data-testid="selected-path" class="path" :title="customPath">{{ customPath }}</p>
          </div>
          <button type="button" class="xdev-btn xdev-btn--secondary button secondary" data-action="browse" :aria-describedby="actionErrorSource === 'browse' ? 'setup-action-error' : undefined" @click="browseDirectory">{{ t('setup.browse_path') }}</button>
        </div>
      </div>

      <div v-else-if="step === 2" class="setup-step">
        <h2 ref="stepTwoHeading" tabindex="-1" data-step-heading="2">{{ t('setup.confirm_config') }}</h2>
        <dl class="path-summary">
          <template v-for="item in directories" :key="item.label">
            <dt>{{ item.label }}</dt><dd class="path" :title="item.path">{{ item.path }}</dd>
          </template>
        </dl>
      </div>

      <div v-else class="setup-step setup-complete">
        <CheckmarkCircleOutline class="success-icon" aria-hidden="true" />
        <h2 ref="stepThreeHeading" tabindex="-1" data-step-heading="3">{{ t('setup.complete') }}</h2>
        <p>{{ t('setup.complete_desc') }}</p>
      </div>

      <p v-if="actionError" id="setup-action-error" ref="actionErrorElement" role="alert" tabindex="-1" class="setup-error">{{ actionError }}</p>
      <footer class="setup-nav">
        <button v-if="step === 2" type="button" class="xdev-btn xdev-btn--secondary button secondary" data-action="back" @click="goToStep(1)">{{ t('btn.back') }}</button>
        <span class="setup-nav__spacer" />
        <button v-if="step === 1" type="button" class="xdev-btn xdev-btn--primary button primary" data-action="next" :disabled="!installBase" @click="goToStep(2)">{{ t('btn.next') }}</button>
        <button v-else-if="step === 2" type="button" class="xdev-btn xdev-btn--primary button primary" data-action="save" :aria-describedby="actionErrorSource === 'save' ? 'setup-action-error' : undefined" :disabled="saving" @click="saveConfig">{{ saving ? t('setup.saving') : t('btn.confirm_save') }}</button>
        <button v-else type="button" class="xdev-btn xdev-btn--primary button primary" data-action="done" @click="emit('done')">{{ t('btn.start_using') }}</button>
      </footer>
    </section>
  </main>
</template>

<script setup>
import { computed, nextTick, onMounted, ref } from 'vue';
import { CheckmarkCircleOutline } from '@vicons/ionicons5';
import DriveOption from './components/setup/DriveOption.vue';
import SetupProgress from './components/setup/SetupProgress.vue';
import { useI18n } from './i18n/index';
import { formatBytes } from './utils/format.js';

const { t } = useI18n();
const emit = defineEmits(['done']);
const step = ref(1), drives = ref([]), selectedDrive = ref(''), customPath = ref('');
const loading = ref(true), saving = ref(false), loadError = ref(''), actionError = ref('');
const actionErrorSource = ref('');
const stepOneHeading = ref(null), stepTwoHeading = ref(null), stepThreeHeading = ref(null), actionErrorElement = ref(null);
const loadErrorElement = ref(null), emptyDrivesElement = ref(null), lastSelection = ref({});
const progressSteps = computed(() => [t('setup.step_location'), t('setup.step_review'), t('setup.step_complete')]);
const installBase = computed(() => customPath.value || (selectedDrive.value ? `${selectedDrive.value}\\XDev` : ''));
// F-3（前端复审，显示层）：分隔符必须与所选路径风格一致——自定义路径（如
// /home/u/xdev）在 step-2 预览里被拼成 \downloads 属于 Windows 假设泄漏；
// Go 侧 InitConfig 用原生 filepath.Join，这里对齐（与已修复的 exportProfile
// 同型问题，仅影响预览不破坏数据）。
const pathSep = computed(() => (installBase.value.includes('\\') ? '\\' : '/'));
const joinBase = (sub) => `${installBase.value}${pathSep.value}${sub}`;
const directories = computed(() => [
  [t('setup.base_dir'), installBase.value], [t('setup.downloads'), joinBase('downloads')],
  [t('setup.installations'), joinBase('install')], [t('setup.shims'), joinBase('shims')],
  [t('setup.cache'), joinBase('cache')],
].map(([label, path]) => ({ label, path })));

function isRecommended(drive) { return drive.letter !== 'C:' && drive.free > 50 * 1024 ** 3; }
function driveSpace(drive) { return `${formatBytes(drive.free)} ${t('setup.free')} / ${formatBytes(drive.total)}`; }
async function goToStep(nextStep) {
  step.value = nextStep;
  await nextTick();
  ({ 1: stepOneHeading, 2: stepTwoHeading, 3: stepThreeHeading })[nextStep].value?.focus();
}
async function showActionError(message, source) {
  actionError.value = message; actionErrorSource.value = source;
  await nextTick();
  actionErrorElement.value?.focus();
}

async function browseDirectory() {
  actionError.value = ''; actionErrorSource.value = '';
  try {
    const directory = await window.go.gui.App.SelectDirectory(t('setup.select_dir'));
    if (directory) { customPath.value = directory; selectedDrive.value = ''; }
  } catch { await showActionError(t('setup.browse_error'), 'browse'); }
}
async function saveConfig() {
  if (saving.value) return;
  saving.value = true; actionError.value = ''; actionErrorSource.value = '';
  try { await window.go.gui.App.InitConfig(installBase.value); await goToStep(3); }
  catch { await showActionError(t('setup.save_error'), 'save'); }
  finally { saving.value = false; }
}
function restoreDriveSelection() {
  const defaultMatch = /^([A-Za-z]:)\\XDev$/i.exec(lastSelection.value.installDir || '');
  const restoredDrive = defaultMatch && drives.value.find((drive) => drive.letter.toLowerCase() === defaultMatch[1].toLowerCase());
  if (restoredDrive) {
    selectedDrive.value = restoredDrive.letter;
    customPath.value = '';
  }
}
async function focusDriveResult() {
  await nextTick();
  if (loadError.value) return loadErrorElement.value?.focus();
  const radios = stepOneHeading.value?.closest('fieldset')?.querySelectorAll('input[type="radio"]');
  const target = [...(radios || [])].find((radio) => radio.checked) || radios?.[0] || emptyDrivesElement.value || stepOneHeading.value;
  target?.focus();
}
async function loadDrives({ restoreFocus = false } = {}) {
  loading.value = true; loadError.value = '';
  try {
    drives.value = await window.go.gui.App.GetDrives();
    restoreDriveSelection();
  } catch { loadError.value = t('setup.load_error'); }
  finally {
    loading.value = false;
    if (restoreFocus) await focusDriveResult();
  }
}
onMounted(async () => {
  const [, selectionResult] = await Promise.allSettled([loadDrives(), window.go.gui.App.GetLastSelection()]);
  if (selectionResult.status === 'rejected') await showActionError(t('setup.restore_error'), 'restore');
  lastSelection.value = selectionResult.status === 'fulfilled' ? selectionResult.value : {};
  if (lastSelection.value.installDir) {
    const defaultMatch = /^([A-Za-z]:)\\XDev$/i.exec(lastSelection.value.installDir);
    if (defaultMatch && drives.value.some((drive) => drive.letter.toLowerCase() === defaultMatch[1].toLowerCase())) {
      selectedDrive.value = drives.value.find((drive) => drive.letter.toLowerCase() === defaultMatch[1].toLowerCase()).letter;
    } else {
      customPath.value = lastSelection.value.installDir;
      selectedDrive.value = '';
    }
  } else if (!loadError.value) {
    selectedDrive.value = drives.value.find((drive) => drive.letter !== 'C:' && drive.free > 20 * 1024 ** 3)?.letter || '';
  }
});
</script>

<style scoped>
.setup-container { min-height: 100vh; display: grid; place-items: start center; padding: 16px; background: var(--xdev-bg-app); overflow-y: auto; }
.setup-card { width: min(640px, calc(100vw - 32px)); padding: clamp(20px, 4vw, 36px); background: var(--xdev-bg-panel); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-lg); box-shadow: var(--xdev-shadow-soft); }
.setup-title { margin: 0 0 6px; color: var(--xdev-accent); font-size: clamp(24px, 4vw, 28px); }
.setup-subtitle { margin: 0 0 22px; color: var(--xdev-text-muted); }
.setup-step { margin-top: 28px; }
.setup-step h2, .drive-fieldset legend { margin: 0; color: var(--xdev-text-primary); font-size: 18px; font-weight: 600; }
.drive-fieldset { min-width: 0; margin: 0; padding: 0; border: 0; }
.setup-hint { margin: 6px 0 16px; color: var(--xdev-text-muted); font-size: 13px; }
.drive-list { display: grid; gap: 8px; }
.custom-path { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-top: 18px; padding-top: 18px; border-top: 1px solid var(--xdev-border-subtle); }
.custom-path p { margin: 5px 0 0; }
.path { min-width: 0; max-width: 100%; overflow-wrap: anywhere; color: var(--xdev-text-secondary); font-family: var(--xdev-font-mono); }
.path-summary { display: grid; grid-template-columns: minmax(90px, auto) minmax(0, 1fr); margin: 16px 0 0; border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-md); }
.path-summary dt, .path-summary dd { margin: 0; padding: 9px 12px; border-bottom: 1px solid var(--xdev-border-subtle); }
.path-summary dt { color: var(--xdev-text-muted); }
.path-summary :nth-last-child(-n+2) { border-bottom: 0; }
.setup-complete { text-align: center; }
.success-icon { width: 64px; color: var(--xdev-success); }
.setup-error { padding: 10px 12px; color: var(--xdev-error); background: var(--xdev-error-soft); border-radius: var(--xdev-radius-md); }
.setup-empty { color: var(--xdev-text-muted); }
.setup-nav { display: flex; align-items: center; gap: 12px; margin-top: 24px; }
.setup-nav__spacer { flex: 1; }
.button { min-height: 36px; padding: 7px 14px; border: 1px solid transparent; border-radius: var(--xdev-radius-md); font: inherit; font-weight: 600; cursor: pointer; }
.button.primary { color: var(--xdev-text-on-accent); background: var(--xdev-accent); }
.button.primary:hover { background: var(--xdev-accent-hover); }
.button.secondary { color: var(--xdev-text-primary); background: transparent; border-color: var(--xdev-border-strong); }
.button:disabled { cursor: not-allowed; opacity: .55; }
@media (max-width: 480px) { .custom-path { align-items: stretch; flex-direction: column; } .path-summary { grid-template-columns: 1fr; } .path-summary dt { padding-bottom: 2px; border-bottom: 0; } .path-summary dd { padding-top: 2px; } }
</style>
