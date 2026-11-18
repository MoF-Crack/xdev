<template>
  <n-config-provider :theme="currentTheme" :theme-overrides="currentThemeOverrides">
    <n-message-provider>
      <n-dialog-provider>
        <main v-if="initializing" class="app-loading" aria-busy="true" aria-label="XDev"><XDevLogo variant="wordmark" /></main><SetupWizard v-else-if="!initialized" @done="onInitDone" /><AppContent v-else />
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>

<script setup>
import { ref, onMounted, onUnmounted, computed } from 'vue';
import { NConfigProvider, NDialogProvider, NMessageProvider } from 'naive-ui';
import SetupWizard from './SetupWizard.vue';
import XDevLogo from './components/XDevLogo.vue';
import AppContent from './AppContent.vue';
import { getNaiveThemeConfig } from './theme/naive-theme.js';
import { DEFAULT_THEME, isKnownTheme } from './theme/themes.js';
import './styles/theme.css';

const initialized = ref(false);
const initializing = ref(true);
const themeName = ref(DEFAULT_THEME);

const currentTheme = computed(() => getNaiveThemeConfig(themeName.value).theme);
const currentThemeOverrides = computed(() => getNaiveThemeConfig(themeName.value).overrides);

function applyTheme() {
  document.documentElement.setAttribute('data-theme', themeName.value);
}

async function checkInit() {
  try {
    initialized.value = await window.go.gui.App.IsInitialized();
  } catch (e) {
    console.error('Check init failed:', e);
  } finally {
    initializing.value = false;
  }
}

function onInitDone() {
  initialized.value = true;
}

function onThemeChange(e) { if (isKnownTheme(e.detail)) { themeName.value = e.detail; applyTheme(); } }
onMounted(() => {
  const stored = localStorage.getItem('xdev-theme');
  themeName.value = stored === 'true' ? 'dark' : stored === 'false' ? 'light' : isKnownTheme(stored) ? stored : DEFAULT_THEME;
  localStorage.setItem('xdev-theme', themeName.value);
  applyTheme();
  window.addEventListener('xdev-theme-change', onThemeChange);
  checkInit();
});
onUnmounted(() => window.removeEventListener('xdev-theme-change', onThemeChange));
</script>

<style>
.app-loading { min-height:100vh; display:grid; place-items:center; background:var(--xdev-bg-app); }
.app-loading svg { width:150px; }
body {
  margin: 0;
  padding: 0;
  font-family: var(--xdev-font-ui);
  background-color: var(--xdev-bg-app);
  color: var(--xdev-text-primary);
}
</style>
