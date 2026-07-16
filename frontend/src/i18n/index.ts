import { ref, computed } from 'vue';
import en from './locales/en';
import zh from './locales/zh';

const locales = { en, zh };
const currentLocale = ref('en');

// Detect language from localStorage or navigator.language
function detectLocale() {
  const stored = localStorage.getItem('locale');
  if (stored && locales[stored]) {
    return stored;
  }
  const navLang = navigator.language || 'en';
  if (navLang.startsWith('zh')) {
    return 'zh';
  }
  return 'en';
}

// Initialize locale on load
currentLocale.value = detectLocale();
document.documentElement.lang = currentLocale.value === 'zh' ? 'zh-CN' : currentLocale.value;

export function useI18n() {
  const locale = computed(() => currentLocale.value);

  function t(key, params) {
    const messages = locales[currentLocale.value] || locales.en;
    let text = messages[key] || locales.en[key] || key;
    if (params) {
      Object.keys(params).forEach((k) => {
        text = text.replace(`{${k}}`, params[k]);
      });
    }
    return text;
  }

  function setLocale(lang) {
    if (locales[lang]) {
      currentLocale.value = lang;
      document.documentElement.lang = lang === 'zh' ? 'zh-CN' : lang;
      localStorage.setItem('locale', lang);
    }
  }

  return { t, locale, setLocale };
}
