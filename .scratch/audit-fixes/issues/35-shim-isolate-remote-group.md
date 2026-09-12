# 35 shim 透传/隔离/远程持久化组（P1-33/34/35）

Status: open
Type: task
批次: B5
对应修复项: P1-33(C-09) + P1-34(C-07) + P1-35(C-08)（方案 §5 主题 E）

## 方案要点

1. **P1-33 排除集归一化**：lookupOnPath 每个非空 PATH 条目做与 addExcludedDir 相同的 Abs+EvalSymlinks 归一化后比对（归一化是主防线；os.SameFile 仅辅助——对 copy 型 shim 无效）；裸名保留末位兜底。
2. **P1-34 nix/container 覆盖防护**：Setup 前目标文件存在 → 拒绝（新增 --force flag）；Teardown 仅删自己写入的——①内容 hash 存 **persistedState**（persist.go:12-17 跨进程 JSON，setup/teardown 两次进程调用）；②flake.lock 由 nix develop 生成，按产物无条件删；③旧存量无 hash → 降级跳过；④container teardown 现状不删 devcontainer.json（复核确认）——只需 Setup 存在性门 + hash 记录。
3. **P1-35 远程实例持久化**：统一 `<home>/instances/<id>.json`；SaveInstance 写 JSON；`remote list` 与 GUI ListRemoteInstances 合并读该目录 + docker ps；旧 `remote/<ID>` marker 兼容读；**不把 SSHTarget 塞进 Metadata**（明文落盘新暴露面）。

## 守卫测试

junction/别名 PATH 条目不再自我 exec 递归；用户已有 flake.nix 不被覆盖；CLI/GUI 首次对 SSH 实例互相可见。
