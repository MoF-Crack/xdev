# 03 resolveShimDir / toolScanCacheFile 绕过 App 配置

Status: resolved
Type: task

`config.Load("")` 再读一遍磁盘，测试隔离目录失效。探测必须用 `a.getCfg()`。
