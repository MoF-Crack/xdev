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

// R8-03（第八轮审计，F-39 修复不完整）：占位符插值必须**一次性扫描**整条文案。
//
// 旧实现逐键 `text.replace(`{${k}}`, () => ...)` 有两个副作用：
//   ① 顺序依赖 —— 上一键的**替换结果**会参与下一键的匹配。参数值里只要含有
//      另一个占位符的字面量，就会被后续键的替换吃掉，而文案里真正的那处占位符
//      反而留在原地：
//        t('env.var_set_success', { name: 'A{tool}B', tool: 'node', level: 'user' })
//          → 'Set AnodeB (tool: {tool}, level: user)'（期望 'Set A{tool}B (tool: node, level: user)'）
//      触发面全部是用户/后端可控数据：环境变量名（env.var_set_success）、
//      后端错误原文（install.activation_warning 的 message）、Windows 路径
//      （legacy.found_content 里的 `{version}` 目录名）。
//   ② 每个键只替换**第一处**匹配 —— 同一占位符在文案里出现两次时第二处不被替换。
//
// 单次正则扫描 + 函数式替换同时解决两者：每个占位符只在**原文位置**上被判定一次，
// 插入的文本不参与后续匹配；函数返回值始终按字面量插入，因此 F-39 修掉的替换模式
// 语义（`$&` / `` $` `` / `$'` / `$$` / `$1`）不会回归。未在 params 中提供的键
// 保留原样占位符（既有行为）。
//
// 导出为纯函数以便直接对任意模板做断言（locale 文案里没有重复占位符的键）。
export function interpolate(text, params) {
  if (!params) return text;
  // `in` 的右操作数必须是对象；params 可能是数字/字符串/数组（既有调用方不保证）。
  const bag = Object(params);
  return String(text).replace(/\{(\w+)\}/g, (match, key) => (
    key in bag ? String(bag[key] ?? '') : match
  ));
}

export function useI18n() {
  const locale = computed(() => currentLocale.value);

  function t(key, params) {
    const messages = locales[currentLocale.value] || locales.en;
    return interpolate(messages[key] || locales.en[key] || key, params);
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
