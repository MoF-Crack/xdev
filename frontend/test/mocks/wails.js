const defaultApp = {
  GetDrives: async () => [],
  GetLastSelection: async () => ({}),
  IsInitialized: async () => false,
  ListCustomTools: async () => [],
  PreviewCustomToolVersions: async () => ({ success: true, message: '', versions: ['v1.0.0'] }),
  AddCustomTool: async () => ({ success: true, message: 'added', versions: ['v1.0.0'] }),
  RemoveCustomTool: async () => {},
};

const defaultRuntime = {
  EventsOn: () => null,
  EventsOff: () => null,
  WindowIsMaximised: async () => false,
};

export function installWailsMocks(appOverrides = {}, runtimeOverrides = {}) {
  window.go = {
    gui: {
      App: { ...defaultApp, ...appOverrides },
    },
  };
  // Spec 037: parallel sessions register multiple listeners per event —
  // defaultRuntime's single-slot EventsOn would drop all but the last.
  //
  // Ticket 12 (audit-fixes / G-01): the mock also counts live/total
  // subscriptions so specs can assert that batch sessions unsubscribe
  // (the leak guard B-1). window.runtime.__subscriptions() returns the
  // counters; EventsOff and the returned unsubscribe function both
  // decrement the live count.
  const unitListeners = {};
  let activeSubscriptions = 0;
  let totalSubscriptions = 0;
  window.runtime = {
    ...defaultRuntime,
    EventsOn: (name, callback) => {
      (unitListeners[name] ||= new Set()).add(callback);
      activeSubscriptions++;
      totalSubscriptions++;
      return () => {
        if (unitListeners[name]?.delete(callback)) activeSubscriptions--;
      };
    },
    EventsOff: (name) => {
      const set = unitListeners[name];
      if (set) {
        activeSubscriptions -= set.size;
        delete unitListeners[name];
      }
    },
    __subscriptions: () => ({ active: activeSubscriptions, total: totalSubscriptions }),
    ...runtimeOverrides,
  };
  // Emit helper for specs: deliver an event to every live listener of the
  // name (mirrors the real Wails runtime fan-out).
  window.__wailsEmit = (name, data) => unitListeners[name]?.forEach((fn) => fn(data));
  return { app: window.go.gui.App, runtime: window.runtime };
}

export function cleanupWailsMocks() {
  delete window.go;
  delete window.runtime;
  delete window.__wailsEmit;
}

export function createWailsBrowserMocks() {
  const app = new Proxy({
    GetDrives: async () => {
      if (window.__failSetupDrives) throw new Error('offline');
      return [
        { letter: 'C:', free: 10737418240, total: 107374182400 },
        { letter: 'D:', free: 85899345920, total: 214748364800 },
      ];
    },
    GetLastSelection: async () => ({}),
    IsInitialized: async () => false,
    SelectDirectory: async () => '\\\\server\\development\\a-very-long-directory-name\\another-long-directory-name\\XDev',
    InitConfig: async (...args) => { window.__wailsCalls.InitConfig.push(args); },
  }, {
    get: (target, property) => target[property] ?? (async () => []),
  });
  const runtime = new Proxy({ WindowIsMaximised: async () => false }, {
    get: (target, property) => target[property] ?? (() => null),
  });
  window.go = { gui: { App: app } };
  window.runtime = runtime;
  window.__wailsCalls = { InitConfig: [] };
}

export function createInitializedWailsBrowserMocks() {
  const tools = [{
    name: 'node',
    displayName: 'Node.js',
    category: 'language',
    description: 'JavaScript runtime built on Chrome V8',
    installed: true,
    activeVersion: '20.18.0',
    localVersion: '20.18.0',
    localPath: 'C:\\Program Files\\nodejs',
    isFavorite: false,
    formFactor: ['gui', 'cli'],
  }, {
    name: 'go',
    displayName: 'Go',
    category: 'language',
    description: 'Fast, statically typed programming language',
    installed: true,
    activeVersion: '1.24.0',
    localVersion: '',
    localPath: '',
    isFavorite: false,
    formFactor: ['cli'],
  }];
  const methods = {
    IsInitialized: async () => true,
    // Spec 032 bootstrap round-trip: AppContent calls GetBootstrapState
    // first on mount. The Proxy fallback would answer [] (no throw, no
    // data) leaving categories/stats/favorites empty — the e2e sidebar
    // never got its dynamic tool pages until this mock existed.
    GetBootstrapState: async () => ({
      initialized: true,
      categories: [{ name: 'language', displayName: 'Languages' }, { name: 'package-manager', displayName: 'Package Managers' }],
      stats: {
        totalTools: 24,
        installedCount: 3,
        installSize: 1610612736,
        cacheSize: 268435456,
        xmdevDir: 'D:\\XDev',
        installDir: 'D:\\XDev\\install',
        shimDir: 'D:\\XDev\\shims',
        cacheDir: 'D:\\XDev\\cache',
        installedTools: [{ displayName: 'Node.js', version: '20.18.0', category: 'language' }],
      },
      favorites: [{ name: 'node', displayName: 'Node.js', category: 'language', addedAt: '2026-08-01', activeVersion: '20.18.0', installed: true, localVersion: '20.18.0', localPath: 'C:\\Program Files\\nodejs', source: 'xdev', formFactor: ['gui', 'cli'] }],
    }),
    GetCategories: async () => [{ name: 'language', displayName: 'Languages' }, { name: 'package-manager', displayName: 'Package Managers' }],
    GetToolsByCategory: async (category) => category === 'package-manager' ? [{ name: 'npm', toolName: 'npm', displayName: 'npm', category: 'package-manager', installed: false, canInstall: true, isFavorite: false }, { name: 'pnpm', toolName: 'pnpm', displayName: 'pnpm', category: 'package-manager', installed: true, canInstall: true, isFavorite: true, hasUpdate: true }, { name: 'manual-pm', toolName: 'manual-pm', displayName: 'Manual PM', category: 'package-manager', installed: false, canInstall: false, installHint: 'Install from vendor site' }] : tools,
    GetDashboardStats: async () => ({
      totalTools: 24,
      installedCount: 3,
      installSize: 1610612736,
      cacheSize: 268435456,
      xmdevDir: 'D:\\XDev',
      installDir: 'D:\\XDev\\install',
      shimDir: 'D:\\XDev\\shims',
      cacheDir: 'D:\\XDev\\cache',
      installedTools: [{ displayName: 'Node.js', version: '20.18.0', category: 'language' }],
    }),
    GetFavorites: async () => [{ name: 'node', displayName: 'Node.js', category: 'language', addedAt: '2026-08-01', activeVersion: '20.18.0', installed: true, localVersion: '20.18.0', localPath: 'C:\\Program Files\\nodejs', source: 'xdev', formFactor: ['gui', 'cli'] }],
    ScanLegacyDir: async () => [],
    GetInstalledVersions: async () => [{ version: '20.18.0', active: true }],
    GetAvailableVersions: async () => [
      { version: '22.14.0', installed: false },
      { version: '20.18.0', installed: true },
    ],
    GetMirrorConfig: async () => ({ node: 'https://nodejs.org/dist' }),
    GetPathMode: async () => 'shim',
    GetProxyConfig: async () => ({ enabled: false, http: '', https: '', noProxy: '' }),
    GetEnvVars: async () => [{ name: 'JAVA_HOME', value: 'C:\\Java', toolName: 'java', level: 'user', source: 'xdev', isValid: true }],
    GetAllEnvVars: async () => [{ name: 'OLD_HOME', value: 'C:\\Missing', toolName: 'old', level: 'user', source: 'system', isValid: false, invalidReason: 'missing' }],
    GetCacheBreakdown: async () => ({ downloads: 134217728, metadata: 1048576, total: 135266304 }),
    InstallTool: async (...args) => { window.__wailsCalls.InstallTool.push(args); },
    // Spec 037 e2e: parallel batch installs go through InstallToolConfirmed.
    InstallToolConfirmed: async (...args) => { window.__wailsCalls.InstallToolConfirmed.push(args); return null; },
    InstallToolWithForm: async (...args) => { window.__wailsCalls.InstallToolWithForm.push(args); },
    PauseDownload: async (...args) => { window.__wailsCalls.PauseDownload.push(args); },
    ResumeDownload: async (...args) => { window.__wailsCalls.ResumeDownload.push(args); },
    CancelDownload: async (...args) => { window.__wailsCalls.CancelDownload.push(args); },
    InstallPackageManager: async () => { window.__wailsCalls.InstallPackageManager++; },
    UpdatePackageManager: async () => { window.__wailsCalls.UpdatePackageManager++; },
    AddFavorite: async () => { window.__wailsCalls.AddFavorite++; },
    RemoveFavorite: async () => { window.__wailsCalls.RemoveFavorite++; },
    SetPathMode: async (...args) => { window.__wailsCalls.SetPathMode.push(args); },
    TestMirrorConnectivity: async (...args) => { window.__wailsCalls.TestMirrorConnectivity.push(args); return { success: true, latency: 12 }; },
    SetMirrorConfig: async (...args) => { window.__wailsCalls.SetMirrorConfig.push(args); },
    SetProxyConfig: async (...args) => { window.__wailsCalls.SetProxyConfig.push(args); },
    TestProxy: async (...args) => { window.__wailsCalls.TestProxy.push(args); return { success: true, message: 'Connected' }; },
    RunDiagnostics: async () => { window.__wailsCalls.RunDiagnostics++; return [{ check: 'PATH', path: 'D:\\XDev', ok: true }]; },
    CleanCache: async (...args) => { window.__wailsCalls.CleanCache.push(args); },
    CheckEnvVarExists: async () => false,
    SetEnvVar: async (...args) => { window.__wailsCalls.SetEnvVar.push(args); },
    DeleteEnvVarEntry: async (...args) => { window.__wailsCalls.DeleteEnvVarEntry.push(args); },
    ScanLocalEnvironmentsForMigration: async () => { window.__wailsCalls.ScanMigration++; return [{ name: 'java', version: '21', path: 'C:\\Java' }]; },
    MigrateLocalToolByJunction: async (...args) => { window.__wailsCalls.MigrateSingle.push(args); },
    MigrateAllLocalTools: async () => { window.__wailsCalls.MigrateAll++; return [{ name: 'java', status: 'success' }]; },
    GetIsolateStatus: async () => ({ dockerAvailable: true, activeMode: 'container' }),
    IsolateSetup: async (...args) => { window.__wailsCalls.IsolateSetup.push(args); return 'created'; },
    IsolateExec: async (...args) => { window.__wailsCalls.IsolateExec.push(args); return 'v22'; },
    IsolateTeardown: async (...args) => { window.__wailsCalls.IsolateTeardown.push(args); },
    ListRemoteInstances: async () => [{ id: 'remote-1', adapter: 'docker', status: 'running', addresses: ['C:\\remote'] }],
    CreateRemoteInstance: async (...args) => { window.__wailsCalls.CreateRemoteInstance.push(args); return { message: 'created' }; },
    ExecRemoteInstance: async (...args) => { window.__wailsCalls.ExecRemoteInstance.push(args); return 'remote output'; },
    DestroyRemoteInstance: async (...args) => { window.__wailsCalls.DestroyRemoteInstance.push(args); },
    GetAboutInfo: async () => ({ appName: 'XDev', version: '1.0.0', buildDate: 'today', goVersion: '1.24', wailsVersion: '2.9', platform: 'windows', arch: 'amd64', repoURL: 'https://example.test/repo', license: 'MIT' }),
    ListCustomTools: async () => [],
    // F-2：Go 契约已改为 {success, message, versions}（失败可携带原因直达 UI）。
    PreviewCustomToolVersions: async (...args) => { window.__wailsCalls.PreviewCustomToolVersions.push(args); return { success: true, message: '', versions: ['v1.0.0', 'v2.0.0'] }; },
    AddCustomTool: async (...args) => { window.__wailsCalls.AddCustomTool.push(args); return { success: true, message: 'added', versions: ['v1.0.0'] }; },
    RemoveCustomTool: async (...args) => { window.__wailsCalls.RemoveCustomTool.push(args); },
    CheckToolDependencies: async (name) => (name === 'node' ? [{ tool: 'npm', requiredVersion: '>=9.0.0', currentVersion: '10.9.0', satisfied: true, reason: '', installPath: '' }] : []),
    IsVersionUnverified: async () => false,
    // Spec 037 e2e: updates list for the dashboard install-all flow.
    // Tests inject window.__wailsUpdates via addInitScript before goto.
    GetAvailableUpdates: async () => (window.__wailsUpdates || []),
    PreviewInstall: async (tool, version) => ({
      tool, version, unverified: false, hasFormSpecificArchives: false, availableForms: [],
      integrity: { tool, version, path: '', ok: false, reason: 'not installed', missing: [] },
    }),
    LaunchTool: async (...args) => { window.__wailsCalls.LaunchTool.push(args); },
    ToolCommand: async (...args) => { window.__wailsCalls.ToolCommand.push(args); return args[0] === 'node' ? 'node' : ''; },
  };
  const app = new Proxy(methods, {
    get: (target, property) => target[property] ?? (async () => []),
  });
  const listeners = {};
  const runtime = new Proxy({
    // Spec 037: multiple sessions listen to the same event name
    // (parallel installs) — keep a Set of callbacks like real Wails.
    EventsOn: (name, callback) => { (listeners[name] ||= new Set()).add(callback); return () => listeners[name]?.delete(callback); },
    EventsOff: (name) => { delete listeners[name]; },
    WindowIsMaximised: async () => false,
  }, {
    get: (target, property) => target[property] ?? (() => null),
  });
  window.__wailsEmit = (name, data) => listeners[name]?.forEach((fn) => fn(data));
  window.__wailsCalls = { InstallTool: [], InstallToolConfirmed: [], InstallToolWithForm: [], PauseDownload: [], ResumeDownload: [], CancelDownload: [], InstallPackageManager: 0, UpdatePackageManager: 0, AddFavorite: 0, RemoveFavorite: 0, SetPathMode: [], TestMirrorConnectivity: [], SetMirrorConfig: [], SetProxyConfig: [], TestProxy: [], RunDiagnostics: 0, CleanCache: [], SetEnvVar: [], DeleteEnvVarEntry: [], ScanMigration: 0, MigrateSingle: [], MigrateAll: 0, IsolateSetup: [], IsolateExec: [], IsolateTeardown: [], CreateRemoteInstance: [], ExecRemoteInstance: [], DestroyRemoteInstance: [], PreviewCustomToolVersions: [], AddCustomTool: [], RemoveCustomTool: [], LaunchTool: [], ToolCommand: [] };
  window.go = { gui: { App: app } };
  window.runtime = runtime;
}
