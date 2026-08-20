# 01 UpdatePackageManager 错名

Status: resolved
Type: task

`UpdatePackageManager` 用 PM 名（pnpm）调 `GetAvailableVersions` / `InstallTool`，catalog 只有 node。HasUpdate 拿 pnpm 版本去和 node latest 比，前端会画出假更新按钮。
