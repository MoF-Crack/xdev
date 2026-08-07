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
	export class CleanResult {
	    name: string;
	    action: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new CleanResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.action = source["action"];
	        this.detail = source["detail"];
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
	export class CustomToolInfo {
	    name: string;
	    displayName: string;
	    sourceUrl: string;
	    sourceType: string;
	
	    static createFrom(source: any = {}) {
	        return new CustomToolInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.displayName = source["displayName"];
	        this.sourceUrl = source["sourceUrl"];
	        this.sourceType = source["sourceType"];
	    }
	}
	export class CustomToolResult {
	    success: boolean;
	    message: string;
	    versions?: string[];
	
	    static createFrom(source: any = {}) {
	        return new CustomToolResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.versions = source["versions"];
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
	export class DependencyStatus {
	    tool: string;
	    requiredVersion: string;
	    currentVersion: string;
	    platform: string;
	    installPath: string;
	    satisfied: boolean;
	    reason: string;
	    reasonCode: string;
	    reasonParams: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new DependencyStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool = source["tool"];
	        this.requiredVersion = source["requiredVersion"];
	        this.currentVersion = source["currentVersion"];
	        this.platform = source["platform"];
	        this.installPath = source["installPath"];
	        this.satisfied = source["satisfied"];
	        this.reason = source["reason"];
	        this.reasonCode = source["reasonCode"];
	        this.reasonParams = source["reasonParams"];
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
	export class DiskUsageEntry {
	    tool: string;
	    version: string;
	    path: string;
	    size: number;
	    isStale?: boolean;
	    category?: string;
	
	    static createFrom(source: any = {}) {
	        return new DiskUsageEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool = source["tool"];
	        this.version = source["version"];
	        this.path = source["path"];
	        this.size = source["size"];
	        this.isStale = source["isStale"];
	        this.category = source["category"];
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
	export class PathEntry {
	    path: string;
	    exists: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PathEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.exists = source["exists"];
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
	    validationStatus: string;
	    pathEntries?: PathEntry[];
	
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
	        this.validationStatus = source["validationStatus"];
	        this.pathEntries = this.convertValues(source["pathEntries"], PathEntry);
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
	    formFactor?: string[];
	
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
	        this.formFactor = source["formFactor"];
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
	export class IgnoredToolInfo {
	    name: string;
	    kind: string;
	    path?: string;
	    ignoredAt: string;
	
	    static createFrom(source: any = {}) {
	        return new IgnoredToolInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.path = source["path"];
	        this.ignoredAt = source["ignoredAt"];
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
	export class InstallMethodInfo {
	    name: string;
	    command: string;
	
	    static createFrom(source: any = {}) {
	        return new InstallMethodInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.command = source["command"];
	    }
	}
	
	export class IntegrityInfo {
	    tool: string;
	    version: string;
	    path: string;
	    ok: boolean;
	    reason: string;
	    missing: string[];
	
	    static createFrom(source: any = {}) {
	        return new IntegrityInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool = source["tool"];
	        this.version = source["version"];
	        this.path = source["path"];
	        this.ok = source["ok"];
	        this.reason = source["reason"];
	        this.missing = source["missing"];
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
	export class PMInstanceInfo {
	    version: string;
	    path: string;
	    managed: boolean;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new PMInstanceInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.path = source["path"];
	        this.managed = source["managed"];
	        this.source = source["source"];
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
	    requiresAdmin: boolean;
	    enableCommand?: string;
	    installMethods?: InstallMethodInfo[];
	    hasUpdate: boolean;
	    instances?: PMInstanceInfo[];
	
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
	        this.requiresAdmin = source["requiresAdmin"];
	        this.enableCommand = source["enableCommand"];
	        this.installMethods = this.convertValues(source["installMethods"], InstallMethodInfo);
	        this.hasUpdate = source["hasUpdate"];
	        this.instances = this.convertValues(source["instances"], PMInstanceInfo);
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
	
	
	export class ProxyProbeResult {
	    url: string;
	    port: number;
	    ok: boolean;
	    latencyMs: number;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new ProxyProbeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.port = source["port"];
	        this.ok = source["ok"];
	        this.latencyMs = source["latencyMs"];
	        this.message = source["message"];
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
	export class SSHTestResult {
	    success: boolean;
	    message: string;
	    latencyMs: number;
	
	    static createFrom(source: any = {}) {
	        return new SSHTestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.latencyMs = source["latencyMs"];
	    }
	}
	export class ServiceStatusInfo {
	    installed: boolean;
	    schedule: string;
	    lastRun: string;
	    nextRun: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new ServiceStatusInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.schedule = source["schedule"];
	        this.lastRun = source["lastRun"];
	        this.nextRun = source["nextRun"];
	        this.error = source["error"];
	    }
	}
	export class SniffConfigInfo {
	    strategy: string;
	    interval: string;
	    concurrency: number;
	    rateLimitPerHr: number;
	    autoArchiveFill: boolean;
	    lastSniffAt: string;
	
	    static createFrom(source: any = {}) {
	        return new SniffConfigInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.strategy = source["strategy"];
	        this.interval = source["interval"];
	        this.concurrency = source["concurrency"];
	        this.rateLimitPerHr = source["rateLimitPerHr"];
	        this.autoArchiveFill = source["autoArchiveFill"];
	        this.lastSniffAt = source["lastSniffAt"];
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
	    hasUpdate: boolean;
	    formFactor?: string[];
	    guiBinary?: string;
	    hasFormSpecificArchives?: boolean;
	    requiresAdmin?: boolean;
	    enableCommand?: string;
	    installMethods?: InstallMethodInfo[];
	    instances?: PMInstanceInfo[];
	
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
	        this.hasUpdate = source["hasUpdate"];
	        this.formFactor = source["formFactor"];
	        this.guiBinary = source["guiBinary"];
	        this.hasFormSpecificArchives = source["hasFormSpecificArchives"];
	        this.requiresAdmin = source["requiresAdmin"];
	        this.enableCommand = source["enableCommand"];
	        this.installMethods = this.convertValues(source["installMethods"], InstallMethodInfo);
	        this.instances = this.convertValues(source["instances"], PMInstanceInfo);
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
	export class UpdateInfo {
	    tool: string;
	    currentVersion: string;
	    latestVersion: string;
	    command: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool = source["tool"];
	        this.currentVersion = source["currentVersion"];
	        this.latestVersion = source["latestVersion"];
	        this.command = source["command"];
	    }
	}
	export class VersionInfo {
	    version: string;
	    installed: boolean;
	    active: boolean;
	    intact: boolean;
	    integrity?: string;
	
	    static createFrom(source: any = {}) {
	        return new VersionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.installed = source["installed"];
	        this.active = source["active"];
	        this.intact = source["intact"];
	        this.integrity = source["integrity"];
	    }
	}

}

