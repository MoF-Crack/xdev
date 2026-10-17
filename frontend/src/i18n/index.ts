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
        // F-39: 必须用函数式替换。字符串形式的第二参数会被 JS 当作替换模式
        // 解释（`$&` / `` $` `` / `$'` / `$$` / `$1`），而参数来自用户数据
        // （环境变量名、工具名、Windows 路径），会把 `$&` 字面量上屏、
        // 用 `$'` 复制匹配点之后的整段文案、或用 `$$` 吃掉字符。
        // 函数返回值永远按字面量插入；null/undefined 退化为空串。
        text = text.replace(`{${k}}`, () => String(params[k] ?? ''));
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
