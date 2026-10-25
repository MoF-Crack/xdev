#!/usr/bin/env bash
# Cross-platform test compilation gate.
#
# Why this script exists (20261021 audit, P1-1):
# `go build ./...` only compiles production files. Windows-only symbols
# referenced from an untagged _test.go are therefore invisible on Windows
# and only blow up when CI runs `go test` on Linux/macOS. `go vet` compiles
# test files too, so a cross-GOOS vet is the cheapest way to catch that.
#
# Prerequisite: frontend/dist must exist (frontend/embed.go uses
# `//go:embed all:dist`). CI builds the frontend before this step; locally
# run `cd frontend && npm run build` once if it is missing.
#
# 2026-10-25 修复（CI 报 undefined: Frontend）：
#
# 原实现无条件 `CGO_ENABLED=0 go vet ./...`。失败链条是：
#   xdev/internal/gui → wails.Run → internal/app
#     → app_production.go  (//go:build production)
#       → internal/frontend/desktop/{linux,darwin}   ← 纯 CGO 包
#          frontend.go 里 `import "C"` + `#cgo pkg-config: gtk+-3.0`，
#          `type Frontend` 就定义在这个文件里。
# 一旦 CGO_ENABLED=0，该文件被整体跳过 → 同包 browser.go / clipboard.go /
# notifications.go 全部 `undefined: Frontend`。
#
# 关键：**不带 `production` 构建标签**时，wails 走 app_dev.go（//go:build dev），
# 不会 import CGO 后端，交叉 vet 即可在无 C 工具链的机器上完成。
#
# 本门禁的职责是「xdev 自己的测试文件能否跨平台编译」，CGO GUI 后端的
# 跨平台构建由 CI 中已装 GTK/WebKit 的 Build CLI 步骤
# （go build -tags desktop,production,webkit2_41）覆盖 —— 那里有真实依赖。
set -euo pipefail

targets="${XDEV_CROSS_COMPILE_TARGETS:-linux/amd64 darwin/arm64}"

for target in $targets; do
  goos="${target%%/*}"
  goarch="${target##*/}"
  echo "== go vet GOOS=${goos} GOARCH=${goarch} =="
  # 刻意不带 production 标签：避免拉入 CGO 的 desktop 后端（见文件头说明）。
  # CGO_ENABLED=0 保证不需要目标平台的 C 工具链。
  GOOS="${goos}" GOARCH="${goarch}" CGO_ENABLED=0 go vet ./...
done

echo "cross-platform test compilation: OK"
