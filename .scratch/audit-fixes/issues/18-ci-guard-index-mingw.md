# 18 CI 守卫 job 四边界 + index 重建 + mingw 处置

Status: blocked（后置于票 17；票 17 因私钥缺失阻塞）
Type: task
批次: B3 数据线
对应修复项: P2-15(D-15) + P2-29 + P0-02 阶段 2 守卫规格（方案 §6 P2-15/P2-29）
Blocked by: 17

## 内容

1. **CI 守卫 job 规格**（P1-12/P0-02 阶段2/P2-15 三处需求合并为一份）：`yaml↔sig 一一对应` + 签名图覆盖全部非空 sha256/mirror_sha256 + index.yaml 差集。**四个边界不定义则守卫第一天就红**：
   - ① index.yaml 无 sig → 豁免；
   - ② 孤儿 sig：placeholder.yaml.sig 与 .placeholder.yaml.sig 无对应 yaml，前者被 main.go:30-31 go:embed 打进二进制——先从 embed 清单移除或补 yaml，再上守卫；
   - ③ sign-manifests 对无哈希工具跳过（:82-86）→ mingw/dsh 需「无哈希豁免」或空图签名，否则差集恒红；
   - ④ mingw 陈旧 sig 处置随 P2-29 分支同步。
2. **D-15**：index.yaml 重建（实测收录 37、缺 15 工具、日期 2026-06-05 过期）——构建脚本生成或守卫差集断言。
3. **P2-29 mingw 处置**：补全（url/format/sha256/strip——remote 指向 niXman 仓库，产物为 **.7z**，引入运行时 7z 依赖需评估 A-03 欠账）或下线（`xdev available` 不再展示）；**archives 缺 url/format 的条目被 LoadCatalog/doctor 标记「不可安装」**（标记先于/后于下线决策——先定分支再动标记）；旧 sig 处置写入分支方案。

## 验收

CI 守卫 job 在当前树 + 重签后全绿；`xdev available` 不再展示不可安装条目（或 mingw 可正常安装）。
