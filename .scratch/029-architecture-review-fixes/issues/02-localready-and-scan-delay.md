# 02 localReady 与 15s 扫描延迟

Status: resolved
Type: task

`loadLocalCache` 填了 localMap 却不 close localReady。空缓存首次启动 `waitForLocalCache` 白等 500ms。全量扫描还要再等 15s。
