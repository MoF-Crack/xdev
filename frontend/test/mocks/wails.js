const defaultApp = {
  GetDrives: async () => [],
  GetLastSelection: async () => ({}),
  IsInitialized: async () => false,
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
  window.runtime = { ...defaultRuntime, ...runtimeOverrides };
  return { app: window.go.gui.App, runtime: window.runtime };
}

export function cleanupWailsMocks() {
  delete window.go;
  delete window.runtime;
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
  }, {
    name: 'go',
    displayName: 'Go',
    category: 'language',
    description: 'Fast, statically typed programming language',
    installed: false,
    localVersion: '',
    localPath: '',
    isFavorite: false,
  }];
  const methods = {
    IsInitialized: async () => true,
    GetCategories: async () => [{ name: 'language', displayName: 'Languages' }, { name: 'package-manager', displayName: 'Package Managers' }],
    GetToolsByCategory: async (category) => category === 'package-manager' ? [{ name: 'npm', toolName: 'npm', displayName: 'npm', installed: false, canInstall: true, isFavorite: false }, { name: 'pnpm', toolName: 'pnpm', displayName: 'pnpm', installed: true, canInstall: true, isFavorite: true }, { name: 'manual-pm', toolName: 'manual-pm', displayName: 'Manual PM', installed: false, canInstall: false, installHint: 'Install from vendor site' }] : tools,
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
    GetFavorites: async () => [],
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
    DeleteEnvVar: async (...args) => { window.__wailsCalls.DeleteEnvVar.push(args); },
    ScanLocalEnvironmentsForMigration: async () => { window.__wailsCalls.ScanMigration++; return [{ name: 'java', version: '21', path: 'C:\\Java' }]; },
    MigrateLocalToolByJunction: async (...args) => { window.__wailsCalls.MigrateSingle.push(args); },
    MigrateAllLocalTools: async () => { window.__wailsCalls.MigrateAll++; return [{ name: 'java', status: 'success' }]; },
    GetProjectConfig: async () => ({ hasConfig: true, dir: 'D:\\demo', tools: { node: '20' }, env: {}, tasks: { build: { run: 'npm run build', desc: 'Build', dir: 'frontend' } } }),
    GetLockfile: async () => { window.__wailsCalls.GetLockfile++; return { found: true, dir: 'D:\\demo', tools: { node: { resolved: '20.18.0', sha256: 'abcdef' } } }; },
    SetProjectTool: async (...args) => { window.__wailsCalls.SetProjectTool.push(args); },
    UnsetProjectTool: async (...args) => { window.__wailsCalls.UnsetProjectTool.push(args); },
    RunProjectTask: async (...args) => { window.__wailsCalls.RunProjectTask.push(args); },
    InstallFromLockfile: async (...args) => { window.__wailsCalls.InstallFromLockfile.push(args); return 'installed'; },
    ListPlugins: async () => [{ name: 'builtin', displayName: 'Built-in plugin' }, { name: 'local', isLua: true, path: 'D:\\XDev\\plugins\\local.lua' }],
    CreatePlugin: async (...args) => { window.__wailsCalls.CreatePlugin.push(args); },
    ListServices: async () => [{ name: 'postgres', status: 'running', type: 'database', version: '16', port: 5432 }, { name: 'redis', status: 'stopped', type: 'cache', version: '7', port: 6379 }],
    StartService: async (...args) => { window.__wailsCalls.StartService.push(args); },
    StopService: async (...args) => { window.__wailsCalls.StopService.push(args); },
    GetAIStatus: async () => ({ available: true, url: 'https://ai.test', model: 'local' }),
    AISuggest: async (...args) => { window.__wailsCalls.AISuggest.push(args); return 'Use 22'; },
    AIDiagnose: async (...args) => { window.__wailsCalls.AIDiagnose.push(args); return 'PATH issue'; },
    AIChat: async (...args) => { window.__wailsCalls.AIChat.push(args); return 'Hello'; },
    GetIsolateStatus: async () => ({ dockerAvailable: true, activeMode: 'container' }),
    IsolateSetup: async (...args) => { window.__wailsCalls.IsolateSetup.push(args); return 'created'; },
    IsolateExec: async (...args) => { window.__wailsCalls.IsolateExec.push(args); return 'v22'; },
    IsolateTeardown: async (...args) => { window.__wailsCalls.IsolateTeardown.push(args); },
    ListRemoteInstances: async () => [{ id: 'remote-1', adapter: 'docker', status: 'running', addresses: ['C:\\remote'] }],
    CreateRemoteInstance: async (...args) => { window.__wailsCalls.CreateRemoteInstance.push(args); return { message: 'created' }; },
    ExecRemoteInstance: async (...args) => { window.__wailsCalls.ExecRemoteInstance.push(args); return 'remote output'; },
    DestroyRemoteInstance: async (...args) => { window.__wailsCalls.DestroyRemoteInstance.push(args); },
    GetCustomTools: async () => [{ name: 'demo', displayName: 'Demo', category: 'system', version: '1', downloadUrl: 'https://example.test/demo.zip' }],
    AddCustomTool: async (...args) => { window.__wailsCalls.AddCustomTool.push(args); },
    InstallCustomTool: async (...args) => { window.__wailsCalls.InstallCustomTool.push(args); },
    RemoveCustomTool: async (...args) => { window.__wailsCalls.RemoveCustomTool.push(args); },
    GetAboutInfo: async () => ({ appName: 'XDev', version: '1.0.0', buildDate: 'today', goVersion: '1.24', wailsVersion: '2.9', platform: 'windows', arch: 'amd64', repoURL: 'https://example.test/repo', license: 'MIT' }),
  };
  const app = new Proxy(methods, {
    get: (target, property) => target[property] ?? (async () => []),
  });
  const listeners = {};
  const runtime = new Proxy({
    EventsOn: (name, callback) => { listeners[name] = callback; return () => { if (listeners[name] === callback) delete listeners[name]; }; },
    EventsOff: (name) => { delete listeners[name]; },
    WindowIsMaximised: async () => false,
  }, {
    get: (target, property) => target[property] ?? (() => null),
  });
  window.__wailsEmit = (name, data) => listeners[name]?.(data);
  window.__wailsCalls = { InstallTool: [], PauseDownload: [], ResumeDownload: [], CancelDownload: [], InstallPackageManager: 0, UpdatePackageManager: 0, AddFavorite: 0, RemoveFavorite: 0, SetPathMode: [], TestMirrorConnectivity: [], SetMirrorConfig: [], SetProxyConfig: [], TestProxy: [], RunDiagnostics: 0, CleanCache: [], SetEnvVar: [], DeleteEnvVar: [], ScanMigration: 0, MigrateSingle: [], MigrateAll: 0, GetLockfile: 0, SetProjectTool: [], UnsetProjectTool: [], RunProjectTask: [], InstallFromLockfile: [], CreatePlugin: [], StartService: [], StopService: [], AISuggest: [], AIDiagnose: [], AIChat: [], IsolateSetup: [], IsolateExec: [], IsolateTeardown: [], CreateRemoteInstance: [], ExecRemoteInstance: [], DestroyRemoteInstance: [], AddCustomTool: [], InstallCustomTool: [], RemoveCustomTool: [] };
  window.go = { gui: { App: app } };
  window.runtime = runtime;
}
