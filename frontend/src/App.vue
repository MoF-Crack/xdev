<template>
  <n-config-provider :theme="currentTheme" :theme-overrides="currentThemeOverrides">
    <n-message-provider>
      <n-dialog-provider>
        <SetupWizard v-if="!initialized" @done="onInitDone" />        <AppContent v-else />
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>

<script setup>
import { ref, onMounted, onUnmounted, computed } from 'vue';
import { darkTheme, lightTheme } from 'naive-ui';
import SetupWizard from './SetupWizard.vue';
import AppContent from './AppContent.vue';
import { getNaiveThemeOverrides } from './theme/naive-theme.js';
import './styles/theme.css';

const initialized = ref(true);
const isDark = ref(true);

const currentTheme = computed(() => isDark.value ? darkTheme : lightTheme);
const currentThemeOverrides = computed(() => getNaiveThemeOverrides(isDark.value ? 'dark' : 'light'));

function applyTheme() {
  document.documentElement.setAttribute('data-theme', isDark.value ? 'dark' : 'light');
}

async function checkInit() {
  try {
    initialized.value = await window.go.gui.App.IsInitialized();
  } catch (e) {
    console.error('Check init failed:', e);
  }
}

function onInitDone() {
  initialized.value = true;
}

function onThemeChange(e) { isDark.value = e.detail; applyTheme(); }
onMounted(() => {
  const stored = localStorage.getItem('xdev-theme');
  if (stored === 'light') {
    isDark.value = false;
  }
  applyTheme();
  window.addEventListener('xdev-theme-change', onThemeChange);
  checkInit();
});
onUnmounted(() => window.removeEventListener('xdev-theme-change', onThemeChange));
</script>

<style>
body {
  margin: 0;
  padding: 0;
  font-family: var(--xdev-font-ui);
  background-color: var(--xdev-bg-app);
  color: var(--xdev-text-primary);
}
</style>
