fix(deps): 升级 labstack/echo v4.13.3 → v4.15.3（Dependabot #29 HIGH）

Dependabot alert #29（HIGH）：echo < 4.15.3 的 %2F 编码斜杠可绕过
route-level 保护并暴露静态文件。echo 是 wails v2.12.0 的间接依赖
（资产服务器）。评估暴露面：Wails 桌面资产服务仅服务本地 webview
（wails:// origin，非网络监听）——对已发布 0.4.0 产物实际风险低；
本修复进入主线，随下个版本发布。

连带升级（echo 4.15.3 的依赖闭包）：gommon 0.4.2→0.5.0、
go-colorable 0.1.13→0.1.14、go-isatty 0.0.20→0.0.22。

验证: go mod vendor 同步后 go build + go vet + go test ./... -short
28 包全绿
