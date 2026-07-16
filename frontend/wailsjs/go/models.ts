export namespace env {
	
	export class LegacyDirInfo {
	    path: string;
	    newPath: string;
	
	    static createFrom(source: any = {}) {
	        return new LegacyDirInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.newPath = source["newPath"];
	    }
	}
	export class LocalTool {
	    name: string;
	    path: string;
	    version: string;
	    binPaths: string[];
	    envVars: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new LocalTool(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.version = source["version"];
	        this.binPaths = source["binPaths"];
	        this.envVars = source["envVars"];
	    }
	}

}

export namespace gui {
	
	export class AIStatus {
	    available: boolean;
	    url: string;
	    model: string;
	
	    static createFrom(source: any = {}) {
	        return new AIStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.url = source["url"];
	        this.model = source["model"];
	    }
	}
	export class AboutInfo {
	    appName: string;
	    version: string;
	    buildDate: string;
	    goVersion: string;
	    wailsVersion: string;
	    platform: string;
	    arch: string;
	    repoURL: string;
	    license: string;
	
	    static createFrom(source: any = {}) {
	        return new AboutInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.appName = source["appName"];
	        this.version = source["version"];
	        this.buildDate = source["buildDate"];
	        this.goVersion = source["goVersion"];
	        this.wailsVersion = source["wailsVersion"];
	        this.platform = source["platform"];
	        this.arch = source["arch"];
	        this.repoURL = source["repoURL"];
	        this.license = source["license"];
	    }
	}
	export class CacheBreakdown {
	    archives: number;
	    shaFiles: number;
	    partFiles: number;
	    scanCache: number;
	    otherFiles: number;
	    totalCount: number;
	
	    static createFrom(source: any = {}) {
	        return new CacheBreakdown(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.archives = source["archives"];
	        this.shaFiles = source["shaFiles"];
	        this.partFiles = source["partFiles"];
	        this.scanCache = source["scanCache"];
	        this.otherFiles = source["otherFiles"];
	        this.totalCount = source["totalCount"];
	    }
	}
	export class CategoryInfo {
	    name: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new CategoryInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.count = source["count"];
	    }
	}
	export class ProxyInfo {
	    http: string;
	    https: string;
	    noProxy: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProxyInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.http = source["http"];
	        this.https = source["https"];
	        this.noProxy = source["noProxy"];
	        this.enabled = source["enabled"];
	    }
	}
	export class ConfigInfo {
	    xmdevDir: string;
	    installDir: string;
	    shimDir: string;
	    cacheDir: string;
	    downloadDir: string;
	    pathMode: string;
	    proxy: ProxyInfo;
	
	    static createFrom(source: any = {}) {
	        return new ConfigInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.xmdevDir = source["xmdevDir"];
	        this.installDir = source["installDir"];
	        this.shimDir = source["shimDir"];
	        this.cacheDir = source["cacheDir"];
	        this.downloadDir = source["downloadDir"];
	        this.pathMode = source["pathMode"];
	        this.proxy = this.convertValues(source["proxy"], ProxyInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CustomTool {
	    name: string;
	    displayName: string;
	    category: string;
	    version: string;
	    downloadUrl: string;
	    installPath: string;
	    shims: string;
	    envVars: string;
	    addedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new CustomTool(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.displayName = source["displayName"];
	        this.category = source["category"];
	        this.version = source["version"];
	        this.downloadUrl = source["downloadUrl"];
	        this.installPath = source["installPath"];
	        this.shims = source["shims"];
	        this.envVars = source["envVars"];
	        this.addedAt = source["addedAt"];
	    }
	}
	export class InstalledToolInfo {
	    name: string;
	    displayName: string;
	    version: string;
	    category: string;
	
	    static createFrom(source: any = {}) {
	        return new InstalledToolInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.displayName = source["displayName"];
	        this.version = source["version"];
	        this.category = source["category"];
	    }
	}
	export class DashboardStats {
	    totalTools: number;
	    installedCount: number;
	    installSize: number;
	    cacheSize: number;
	    shimCount: number;
	    xmdevDir: string;
	    installDir: string;
	    shimDir: string;
	    cacheDir: string;
	    downloadDir: string;
	    installedTools: InstalledToolInfo[];
	
	    static createFrom(source: any = {}) {
	        return new DashboardStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.totalTools = source["totalTools"];
	        this.installedCount = source["installedCount"];
	        this.installSize = source["installSize"];
	        this.cacheSize = source["cacheSize"];
	        this.shimCount = source["shimCount"];
	        this.xmdevDir = source["xmdevDir"];
	        this.installDir = source["installDir"];
	        this.shimDir = source["shimDir"];
	        this.cacheDir = source["cacheDir"];
	        this.downloadDir = source["downloadDir"];
	        this.installedTools = this.convertValues(source["installedTools"], InstalledToolInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DebugInfo {
	    xmdevDir: string;
	    manifestDir: string;
	    cacheDir: string;
	    registryCount: number;
	    catalogCount: number;
	    registryNames: string[];
	    catalogNames: string[];
	
	    static createFrom(source: any = {}) {
	        return new DebugInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.xmdevDir = source["xmdevDir"];
	        this.manifestDir = source["manifestDir"];
	        this.cacheDir = source["cacheDir"];
	        this.registryCount = source["registryCount"];
	        this.catalogCount = source["catalogCount"];
	        this.registryNames = source["registryNames"];
	        this.catalogNames = source["catalogNames"];
	    }
	}
	export class DiagnosticResult {
	    check: string;
	    path: string;
	    ok: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new DiagnosticResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.check = source["check"];
	        this.path = source["path"];
	        this.ok = source["ok"];
	        this.error = source["error"];
	    }
	}
	export class DownloadTaskInfo {
	    id: string;
	    tool: string;
	    version: string;
	    status: string;
	    progress: number;
	    speed: string;
	    eta: string;
	    downloaded: number;
	    total: number;
	    errorMessage?: string;
	
	    static createFrom(source: any = {}) {
	        return new DownloadTaskInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.tool = source["tool"];
	        this.version = source["version"];
	        this.status = source["status"];
	        this.progress = source["progress"];
	        this.speed = source["speed"];
	        this.eta = source["eta"];
	        this.downloaded = source["downloaded"];
	        this.total = source["total"];
	        this.errorMessage = source["errorMessage"];
	    }
	}
	export class DriveInfoJSON {
	    letter: string;
	    path: string;
	    free: number;
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new DriveInfoJSON(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.letter = source["letter"];
	        this.path = source["path"];
	        this.free = source["free"];
	        this.total = source["total"];
	    }
	}
	export class EnvVarInfo {
	    name: string;
	    value: string;
	    source: string;
	    level: string;
	    toolName: string;
	    isValid: boolean;
	    invalidReason: string;
	
	    static createFrom(source: any = {}) {
	        return new EnvVarInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.value = source["value"];
	        this.source = source["source"];
	        this.level = source["level"];
	        this.toolName = source["toolName"];
	        this.isValid = source["isValid"];
	        this.invalidReason = source["invalidReason"];
	    }
	}
	export class FavoriteInfo {
	    name: string;
	    displayName: string;
	    category: string;
	    addedAt: string;
	    activeVersion: string;
	    installed: boolean;
	    localVersion: string;
	    localPath: string;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new FavoriteInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.displayName = source["displayName"];
	        this.category = source["category"];
	        this.addedAt = source["addedAt"];
	        this.activeVersion = source["activeVersion"];
	        this.installed = source["installed"];
	        this.localVersion = source["localVersion"];
	        this.localPath = source["localPath"];
	        this.source = source["source"];
	    }
	}
	export class FixedVersion {
	    tool: string;
	    oldVer: string;
	    newVer: string;
	
	    static createFrom(source: any = {}) {
	        return new FixedVersion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool = source["tool"];
	        this.oldVer = source["oldVer"];
	        this.newVer = source["newVer"];
	    }
	}
	export class ImportResult {
	    success: boolean;
	    message: string;
	    installed: number;
	    failed: number;
	    errors: string[];
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.installed = source["installed"];
	        this.failed = source["failed"];
	        this.errors = source["errors"];
	    }
	}
	
	export class IsolateStatus {
	    availableModes: string[];
	    activeMode: string;
	    containerId: string;
	    dockerAvailable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new IsolateStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.availableModes = source["availableModes"];
	        this.activeMode = source["activeMode"];
	        this.containerId = source["containerId"];
	        this.dockerAvailable = source["dockerAvailable"];
	    }
	}
	export class LocalToolInfo {
	    name: string;
	    displayName: string;
	    category: string;
	    version: string;
	    path: string;
	    managed: boolean;
	    activeVersion: string;
	
	    static createFrom(source: any = {}) {
	        return new LocalToolInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.displayName = source["displayName"];
	        this.category = source["category"];
	        this.version = source["version"];
	        this.path = source["path"];
	        this.managed = source["managed"];
	        this.activeVersion = source["activeVersion"];
	    }
	}
	export class LockfileToolInfo {
	    version: string;
	    resolved: string;
	    sha256: string;
	    url: string;
	    installPath: string;
	
	    static createFrom(source: any = {}) {
	        return new LockfileToolInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.resolved = source["resolved"];
	        this.sha256 = source["sha256"];
	        this.url = source["url"];
	        this.installPath = source["installPath"];
	    }
	}
	export class LockfileInfo {
	    dir: string;
	    found: boolean;
	    tools: Record<string, LockfileToolInfo>;
	
	    static createFrom(source: any = {}) {
	        return new LockfileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dir = source["dir"];
	        this.found = source["found"];
	        this.tools = this.convertValues(source["tools"], LockfileToolInfo, true);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class MigrationResult {
	    name: string;
	    status: string;
	    version?: string;
	    path?: string;
	    error?: string;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new MigrationResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.status = source["status"];
	        this.version = source["version"];
	        this.path = source["path"];
	        this.error = source["error"];
	        this.reason = source["reason"];
	    }
	}
	export class PackageManagerInfo {
	    name: string;
	    displayName: string;
	    description: string;
	    category: string;
	    installed: boolean;
	    version: string;
	    localPath: string;
	    canInstall: boolean;
	    toolName: string;
	    installHint: string;
	
	    static createFrom(source: any = {}) {
	        return new PackageManagerInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.displayName = source["displayName"];
	        this.description = source["description"];
	        this.category = source["category"];
	        this.installed = source["installed"];
	        this.version = source["version"];
	        this.localPath = source["localPath"];
	        this.canInstall = source["canInstall"];
	        this.toolName = source["toolName"];
	        this.installHint = source["installHint"];
	    }
	}
	export class PluginInfo {
	    name: string;
	    displayName: string;
	    category: string;
	    path: string;
	    isLua: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PluginInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.displayName = source["displayName"];
	        this.category = source["category"];
	        this.path = source["path"];
	        this.isLua = source["isLua"];
	    }
	}
	export class TaskInfo {
	    run: string;
	    desc: string;
	    dir: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.run = source["run"];
	        this.desc = source["desc"];
	        this.dir = source["dir"];
	    }
	}
	export class ProjectConfigInfo {
	    dir: string;
	    tools: Record<string, string>;
	    env: Record<string, string>;
	    tasks: Record<string, TaskInfo>;
	    hasConfig: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProjectConfigInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dir = source["dir"];
	        this.tools = source["tools"];
	        this.env = source["env"];
	        this.tasks = this.convertValues(source["tasks"], TaskInfo, true);
	        this.hasConfig = source["hasConfig"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class ProxyTestResult {
	    success: boolean;
	    message: string;
	    latency: number;
	
	    static createFrom(source: any = {}) {
	        return new ProxyTestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.latency = source["latency"];
	    }
	}
	export class RemoteCreateResult {
	    id: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteCreateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.message = source["message"];
	    }
	}
	export class RemoteInstanceInfo {
	    id: string;
	    status: string;
	    addresses: string[];
	    adapter: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteInstanceInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.status = source["status"];
	        this.addresses = source["addresses"];
	        this.adapter = source["adapter"];
	    }
	}
	export class ServiceInfo {
	    name: string;
	    type: string;
	    version: string;
	    status: string;
	    port: number;
	
	    static createFrom(source: any = {}) {
	        return new ServiceInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.version = source["version"];
	        this.status = source["status"];
	        this.port = source["port"];
	    }
	}
	
	export class ToolInfo {
	    name: string;
	    displayName: string;
	    category: string;
	    description: string;
	    defaultVersion: string;
	    activeVersion: string;
	    installed: boolean;
	    localVersion: string;
	    localPath: string;
	    canInstall: boolean;
	    toolName: string;
	    installHint: string;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new ToolInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.displayName = source["displayName"];
	        this.category = source["category"];
	        this.description = source["description"];
	        this.defaultVersion = source["defaultVersion"];
	        this.activeVersion = source["activeVersion"];
	        this.installed = source["installed"];
	        this.localVersion = source["localVersion"];
	        this.localPath = source["localPath"];
	        this.canInstall = source["canInstall"];
	        this.toolName = source["toolName"];
	        this.installHint = source["installHint"];
	        this.source = source["source"];
	    }
	}
	export class UpdateCheckResult {
	    hasUpdate: boolean;
	    current: string;
	    latest: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateCheckResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hasUpdate = source["hasUpdate"];
	        this.current = source["current"];
	        this.latest = source["latest"];
	        this.error = source["error"];
	    }
	}
	export class VersionInfo {
	    version: string;
	    installed: boolean;
	    active: boolean;
	
	    static createFrom(source: any = {}) {
	        return new VersionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.installed = source["installed"];
	        this.active = source["active"];
	    }
	}

}

