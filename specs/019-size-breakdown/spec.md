# Spec 019: 安装/缓存磁盘占用明细

> **状态**：已完成
> **类型**：功能新增 + 优化
> **日期**：2026-08-01

## 问题

仪表盘顶部指标卡片显示 `安装大小: 9.6 GB`、`缓存大小: 1.3 GB`，但用户无法得知：
- 具体是哪些工具、哪些版本占用了安装空间
- 缓存里每个工具的归档文件占了多少

用户希望点击对应卡片后看到按工具/版本拆分的明细表。

## 现状

| 组件 | 现状 |
|------|------|
| `DashboardPage.vue` | 顶部 4 个 `MetricCard`（工具总数/已安装/安装大小/缓存大小），不可点击 |
| `MetricCard.vue` | 纯展示组件（label + value），无点击能力 |
| `GetDashboardStats` | 已有 `InstallSize` / `CacheSize` 总量（`dirSizeFast` 递归统计） |
| `GetCacheBreakdown` | 已有按**扩展名**分类的缓存统计（.zip/.tar.gz/.sha256/.part） |
| 磁盘布局 | `install/<tool>/<version>/...`；缓存文件 `cache/<tool>-<version>.<ext>`（如 `java-17.zip`、`llvm-22.1.8.tar.xz`） |

## 修复

### F1: 后端 — 安装占用明细

`internal/gui/app_local_detect.go`（或新文件 `internal/gui/app_size_breakdown.go`）新增：

```go
// DiskUsageEntry 单个安装版本/缓存文件的占用。
type DiskUsageEntry struct {
    Tool    string `json:"tool"`    // xdev 工具名（如 "go"）
    Version string `json:"version"` // 版本目录名（如 "1.26.5"）；安装占用必填
    Path    string `json:"path"`    // 绝对路径
    Size    int64  `json:"size"`    // 字节数
    // IsStale 缓存明细专用：true 表示无对应已安装版本（孤儿缓存）
    IsStale bool   `json:"isStale,omitempty"`
}

// GetInstallSizeBreakdown 返回 install/<tool>/<version> 各级占用，按 Size 降序。
func (a *App) GetInstallSizeBreakdown() []DiskUsageEntry
```

实现要点：
- 遍历 `InstallDir` 下每个工具目录的子目录（版本目录），对每个版本目录用 `dirSizeFast` 统计
- 跳过 `local` 等非版本目录？——不跳过，全部列出（version 字段即目录名，用户可见）
- 按 Size 降序排序
- 复用现有 `dirSizeFast`（`app_local_detect.go:1189`）

### F2: 后端 — 缓存占用明细

```go
// GetCacheSizeBreakdown 返回 cache/ 下各工具归档文件占用，按 Size 降序。
func (a *App) GetCacheSizeBreakdown() []DiskUsageEntry
```

实现要点：
- 遍历 `CacheDir` 下所有文件（跳过目录）
- 从文件名解析工具名：`<tool>-<version>.<ext>`，取第一个 `-` 前的部分作 `Tool`（如 `java-17.zip` → `java`）；解析失败归入 `Tool="-"`，`Version=""`
- `Version` 取 `<tool>-` 与最后扩展名之间的部分（如 `17`、`22.1.8`）；无法解析时留空
- `Path` 为完整路径，`Size` 为文件大小
- 若该文件无对应已安装版本（`install/<tool>/<version>` 不存在），置 `IsStale=true`
- 排除 `*.lock`、`*.dat`、`*.json` 等非工具归档？——不排除，全部列出并按解析结果归类；无法解析的归 `Tool="-"`

### F3: 前端 — 可点击卡片 + 明细弹层

- `MetricCard.vue`：新增可选 `clickable` prop 与 `@click` 透传；有值时渲染为 `<button>`（键盘可达、focus 样式），样式保持卡片外观，hover 提示可点击（加细微边框/光标）
- `DashboardPage.vue`：
  - 「安装大小」「缓存大小」两张卡片绑定点击事件，其他卡片不可点
  - 点击后调用 `window.go.gui.App.GetInstallSizeBreakdown()` / `GetCacheSizeBreakdown()`
  - 用 Naive UI `n-drawer`（右侧抽屉）或 `n-modal` 展示明细表格：`工具` | `版本` | `路径` | `大小`，按大小降序，顶部显示合计
  - 缓存抽屉中 `IsStale` 行加「未安装」小标记
  - 空结果时显示空状态；加载中显示 loading
  - 抽屉可关闭（ESC/遮罩/关闭按钮）
- `AppContent.vue`：新增 `installSizeEntries` / `cacheSizeEntries` ref 与拉取函数，透传给 DashboardPage

### F4: i18n

`zh.ts` / `en.ts` 新增：
- `dashboard.install_size_detail`: 安装占用明细 / Install Size Details
- `dashboard.cache_size_detail`: 缓存占用明细 / Cache Size Details
- `dashboard.size_total`: 合计 / Total
- `dashboard.size_stale`: 未安装 / Not installed
- 复用 `table.tool` / `table.version` / `table.local_path` / `table.size`（若无 size 则新增）

## 验证

1. `go test ./internal/gui/ -short`（新增 GetInstallSizeBreakdown / GetCacheSizeBreakdown 单元测试：临时目录造数据，断言排序与字段）
2. 前端 `vite build` + 相关 vitest（如有）
3. 手动：仪表盘点「安装大小」→ 抽屉显示 go/1.26.5、llvm/22.1.8 等明细；点「缓存大小」→ 显示 java-17.zip 等，且孤儿文件标记「未安装」
4. 合计与卡片数值一致
5. `wails build`

---

## ���� 4: �������� + ��װ�������

### ����

1. ����ռ����ϸ����ֻ��չʾ���޷������������ļ�����������Ļ����޷����Ǳ���ֱ��ɾ��
2. ��װռ����ϸ�Ǳ�ƽ�б������й��߻��ţ����û�ϣ���񻺴����һ�������ࣨ���ߣ�����չʾ

### F6: ��� �� ����ɾ��

`internal/gui/app_config.go`���� `app_size_breakdown.go`��������

```go
// DeleteCacheFile ɾ�����������ļ��������� .sha256/.part����
// path ����λ�� CacheDir �ڣ���Ŀ¼��Խ����
func (a *App) DeleteCacheFile(path string) error

// CleanCacheCategory ������ɾ�������ļ����� GetCacheBreakdown ����һ�£���
// category: "archives" | "shaFiles" | "partFiles" | "scanCache" | "otherFiles" | "all"
func (a *App) CleanCacheCategory(category string) error
```

- `DeleteCacheFile`��У�� path �� CacheDir �ڣ�filepath.Rel + ValidateSubPath ˼·����ɾ���ļ����� + ͬ�� `.sha256`/`.part` �������ɹ��� `InvalidateSizeBreakdown()`
- `CleanCacheCategory`������ `isArchiveName` / `.sha256` / `.part` / `tool_scan_cache.json` ��������� GetCacheBreakdown ��ȫһ�£���scanCache ֻɾ `tool_scan_cache.json`��otherFiles ɾ����Ԫ�����ļ���json/dat/lock �ȣ�**����Ŀ¼**�����ɹ��� `InvalidateSizeBreakdown()`
- ���� `CleanCache`��archives/shaFiles/partFiles/all��������ί�и��·���

### F7: ǰ�� �� �����������

`DashboardPage.vue` ������룺
- ��һ�������У��Ҳ�ӡ���������ť��ConfirmAction ȷ�ϣ������ emit `clean-cache-category`(category)
- �ڶ����ļ��У��Ҳ�ӡ�ɾ������ť��ConfirmAction ȷ�ϣ���emit `delete-cache-file`(entry)
- `AppContent.vue`������ `cleanCacheCategory(category)` / `deleteCacheFile(entry)` �������������º�˷�����������ȡ `GetCacheSizeBreakdown()` + `loadDashboard()`
- i18n��`cache.delete_file` ɾ�� / Delete��`cache.clean_category` �������� / Clean category��`cache.delete_confirm` ȷ��ɾ�����ļ��� / Delete this file?��`cache.clean_category_confirm` ȷ�������÷��໺�棿 / Clean this category cache?

### F8: ǰ�� �� ��װ��ϸ�������

`DashboardPage.vue` ��װ�����Ϊ�뻺�����һ�µ�������
- ��һ������**����**���飨tool �� + �汾�� + �ϼƴ�С�����̶�˳�����ƻ�ϼƽ���
- �ڶ������ù��ߵĸ��汾�ļ����汾 | ·�� | ��С������������С/�����������л�������ģʽ���ѷ���ɼ�Ϊ�汾������
- ������������ֶΣ�install entries ���� tool/version/path/size��
- ���� `cacheCategoryLabelKey` ˼·��install ���༴����������ʾԭʼ tool �������� i18n ӳ�䣩

### ��֤

1. `go test ./internal/gui/ -short`������ DeleteCacheFile ·����Խ������CleanCacheCategory �����������ԣ�
2. ǰ�� `vite build` + DashboardPage vitest
3. �ֶ����������ɾ����/�������б���ʱˢ�£���װ���밴���߷���չ��
4. `wails build`


## 批准记录 (Approval History)

> **批准ID**: `2026-08-08-spec-019-draft-001`
