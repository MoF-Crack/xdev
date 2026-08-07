/**
 * audit.config.mjs - XDev project config for the ui-audit skill.
 * See E:\aiConfig\opencode\skills\ui-audit\ for the runner & detectors.
 */
export default {
  mode: 'mock',

  // Vite dev server lives in frontend/ (package.json is there)
  entry: 'npm run dev -- --host 127.0.0.1 --port 4173',
  entryCwd: 'frontend',
  url: 'http://127.0.0.1:4173',

  // Real Wails shell (build/bin/xdev-gui.exe from wails.json outputfilename)
  webview2: {
    exe: 'build/bin/xdev-gui.exe',
    port: 9222,
  },

  // Pages from frontend/src/navigation/navigation.js staticGroups
  pages: [
    'dashboard',
    'favorites',
    'env-vars',
    'isolate',
    'remote',
    'settings',
    'about',
  ],

  navigation: '[data-view]',
  clickable: 'button, [data-action], [role="tab"], a[href], [role="checkbox"], [role="radio"]',

  // Safety: never auto-click destructive actions
  skip: [
    'cache-clean-all',
    'uninstall',
    'remove',
    'delete',
    'reset',
  ],

  // XDev records bound Wails API calls on window.__wailsCalls (used by e2e)
  apiRecorder: '__wailsCalls',

  // Category contract from navigation.js groups
  categories: {
    dashboard: 'Tools',
    favorites: 'Tools',
    'env-vars': 'Environment',
    isolate: 'Development',
    remote: 'Development',
    settings: 'Settings',
    about: 'Settings',
  },

  viewports: [
    { width: 640, height: 720 },
    { width: 1440, height: 900 },
  ],

  // Match existing e2e baseline conventions
  locale: 'zh',
  theme: 'dark',

  // Frontend deps live in frontend/node_modules (playwright is there)
  depsDirs: ['frontend'],

  detectors: [],
  setupScript: null,
};
