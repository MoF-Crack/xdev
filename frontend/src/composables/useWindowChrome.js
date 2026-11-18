import { onMounted, onUnmounted, ref } from 'vue';
import { Quit, WindowIsMaximised, WindowMinimise, WindowToggleMaximise } from '../desktop/window.js';

export function useWindowChrome() {
  const isMaximized = ref(false);

  function minimizeWindow() {
    WindowMinimise();
  }

  async function toggleMaximize() {
    WindowToggleMaximise();
    try {
      isMaximized.value = await WindowIsMaximised();
    } catch (error) {
      console.warn('[XDev] Failed to read maximized window state:', error);
    }
  }

  function closeWindow() {
    Quit();
  }

  let maximizePollTimer = null;
  onMounted(() => {
    maximizePollTimer = setInterval(() => {
      WindowIsMaximised()
        .then(v => { isMaximized.value = v; })
        .catch(error => console.warn('[XDev] Failed to poll maximized window state:', error));
    }, 500);
  });
  onUnmounted(() => {
    if (maximizePollTimer) clearInterval(maximizePollTimer);
  });

  return { isMaximized, minimizeWindow, toggleMaximize, closeWindow };
}
