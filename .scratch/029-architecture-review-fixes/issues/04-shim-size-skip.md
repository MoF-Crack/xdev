# 04 Windows shim 用 size 跳过拷贝

Status: resolved
Type: task

同 size 就当已最新。升级后二进制碰巧同大小时 shim 仍跑旧 dispatcher。改为 size+mtime。
