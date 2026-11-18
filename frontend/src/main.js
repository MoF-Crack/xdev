import { createApp } from 'vue';
import App from './App.vue';

// Browser-only tests may inject a complete fake before the page loads. Native
// desktop builds always use the generated bindings and the pinned v3 runtime.
if (!window.go?.gui?.App) {
  const [bindings, runtime, bridge] = await Promise.all([
    import('../bindings/github.com/xmdemt/xdev/internal/gui/app'),
    import('@wailsio/runtime'),
    import('./desktop/bridge.js'),
  ]);
  bridge.installDesktopBridge(window, bridge.createDesktopBridge(bindings, runtime));
}
const app = createApp(App);
app.mount('#app');
