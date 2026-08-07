﻿# Spec 023 Checklist锛堥€愰」楠屾敹鏍囧噯锛?
> **状态**：完成（Phase A-E 全部通过，LOOP 收敛）
> **鍓嶇疆**锛歋pec 023 + Plan + Task 宸茶幏鎵?> **鏃ユ湡**锛?026-08-02
> **浣跨敤**锛氭瘡椤规弧瓒冲悗鍦ㄣ€岄獙鏀躲€嶆墦鍕惧苟璁板綍锛涘叏閮ㄩ€氳繃鍚庤繘鍏?LOOP 瀹炴柦銆?
---

## Phase A 鈥?骞冲彴鑳藉姏

### T-A1 depends 渚濊禆澹版槑
- [ ] `Tool.Depends []Dependency` + `Dependency{Tool,MinVersion,Platforms,InstallPath}`锛坥mitempty锛?- [ ] 鍔犺浇鏍￠獙锛歵ool 闈炵┖锛沵in_version 鍙?ParseSemverParts锛沺latforms 鈭?{windows,darwin,linux,any}锛堥潪娉曚粎璀﹀憡锛?- [ ] 鐜版湁 39 manifest 鏃?depends 姝ｅ父鍔犺浇锛堝悜鍚庡吋瀹癸級
- [ ] 娴嬭瘯锛氬悎娉曡В鏋?/ 闈炴硶瀹归敊 / 鍏煎鎬?- [ ] **楠屾敹**锛歚go test ./internal/manifest/ -count=1 -short` 閫氳繃锛屾棤鍥炲綊

### T-A2 npm 杩滅▼绫诲瀷
- [ ] `RemoteSource.Package string`锛坹aml:"package"锛?- [ ] npm 瑙ｆ瀽锛欸ET registry 鈫?dist-tags.latest + versions keys 鈫?semver 闄嶅簭鎴柇 20
- [ ] typescript.yaml 璧?npm 鍒锋柊鎴愬姛锛堝洖褰掍慨澶嶏級
- [ ] 娴嬭瘯锛歨ttptest mock registry锛坴ersions/dist-tags/latest/閿欒璺緞锛夛紱闈?npm 绫诲瀷涓嶅彈褰卞搷
- [ ] **楠屾敹**锛歚go test ./internal/remote/ ./internal/manifest/ -count=1 -short` 閫氳繃

### T-A3 dmg 瑙ｅ寘
- [ ] `case "dmg"`锛歮acOS hdiutil attach鈫抍opy鈫抎etach锛涢潪 macOS 鏄庣‘鎶ラ敊
- [ ] 娴嬭瘯 hook锛堝悓 zst 妯″紡锛塵ock hdiutil
- [ ] **楠屾敹**锛歚go test ./internal/extractor/ -count=1 -short` 閫氳繃

### T-A4 {{install}} 妯℃澘
- [ ] InstallArgs 鏀寔 `{{install}}` 鈫?installPath锛坽{version}}/{{tool}} 淇濈暀锛?- [ ] 娴嬭瘯锛歮ock runner 鏂█ args 鍚湡瀹?installPath
- [ ] **楠屾敹**锛歚go test ./internal/installer/ -count=1 -short -run "RunInstaller|Template"` 閫氳繃

### T-A5 鎻掍欢娉ㄥ唽娉涘寲
- [ ] register.go 鐢?catalog 鍏ㄩ噺鐢熸垚鎻掍欢锛堢Щ闄ょ‖缂栫爜娓呭崟锛?- [ ] 鐜版湁宸ュ叿娉ㄥ唽琛屼负涓嶅彉
- [ ] 娴嬭瘯锛氫复鏃?catalog 鏉＄洰鑷姩娉ㄥ唽
- [ ] **楠屾敹**锛歚go test ./internal/cli/ -count=1 -short` 閫氳繃

### T-A6 褰㈡€佹ā鍨?- [ ] `Tool.FormFactor []string` + `Tool.GUIBinary string`锛沗EffectiveFormFactor()`锛堢己鐪?shims鈫抂cli]锛?- [ ] integrity锛氬惈 gui + gui_binary 鈫?`<install>/<gui_binary>` 鍊欓€夋牎楠?- [ ] `ToolInfo.FormFactor` + `GUIBinary` json 杈撳嚭
- [ ] 娴嬭瘯锛氭帹鏂鍒?/ gui_binary 鍛戒腑涓庣己澶?/ JSON 瀛楁
- [ ] **楠屾敹**锛歚go test ./internal/manifest/ ./internal/installer/ ./internal/gui/ -count=1 -short` 閫氳繃

## Phase B 鈥?渚濊禆妫€鏌?
### T-B1 CheckToolDependencies
- [ ] `DependencyStatus{Tool,RequiredVersion,CurrentVersion,Platform,InstallPath;Satisfied;Reason}`
- [ ] 鍒ゅ畾閾撅細platform 鈫?瀛樺湪鎬э紙xdev 宸茶浼樺厛锛屽惁鍒?LookPath 璺宠繃 shim锛夆啋 鐗堟湰锛圕ompareSemver 鈮ワ級
- [ ] 娴嬭瘯锛歯ode 缂哄け / PATH 鍛戒腑 / xdev 宸茶 / platform 涓嶅尮閰?/ 鏈煡宸ュ叿
- [ ] **楠屾敹**锛歚go test ./internal/gui/ -count=1 -short -run "Dependency|CheckToolDependencies"` 閫氳繃

### T-B2 鍓嶇渚濊禆鎻愮ず
- [ ] 璇︽儏椤靛畨瑁呭尯渚濊禆寰芥爣锛堚渽/鈿狅笍 闇€瑕?Node 鈮?22锛夛紝鎵撳紑璇︽儏鏃惰皟 CheckToolDependencies
- [ ] i18n锛歚detail.deps_satisfied/missing/platform/install_hint`锛坺h/en锛?- [ ] **楠屾敹**锛歚npx vite build` + 鐩稿叧 vitest 閫氳繃

## Phase C 鈥?鍚姩涓庡舰鎬?UI

### T-C1 LaunchTool / ToolCommand
- [ ] `LaunchTool`锛歝urrent/<tool> 鈫?gui_binary 鈫?exec锛圖ir=installPath锛孍nv=Activate 娉ㄥ叆锛?- [ ] `ToolCommand`锛氳繑鍥?shims[0]
- [ ] 娴嬭瘯锛歡ui_binary 缂哄け鎶ラ敊 / ToolCommand 姝ｇ‘
- [ ] **楠屾敹**锛歚go test ./internal/gui/ -count=1 -short -run "Launch|ToolCommand"` 閫氳繃

### T-C2 褰㈡€佸窘鏍囦笌鎿嶄綔鎸夐挳
- [ ] 鍗＄墖/琛ㄦ牸褰㈡€佸窘鏍囷紙GUI/CLI/TUI锛?- [ ] 璇︽儏椤碉細鍚?gui 鈫掋€屽惎鍔ㄣ€嶏紱浠?cli/tui 鈫掋€屽鍒跺懡浠ゃ€嶏紙clipboard锛?- [ ] i18n锛歚detail.launch/copy_command/copied` + `form.gui/cli/tui`
- [ ] **楠屾敹**锛歚npx vite build` + 鐩稿叧 vitest 閫氳繃

## Phase D 鈥?棣栨壒 manifest

**缁熶竴楠屾敹锛堟瘡椤癸級**锛歮anifest 瑙ｆ瀽閫氳繃锛沀RL HEAD 200锛汼HA 瀹炴祴鍥炲～锛涘悓姝ヨ繍琛屾椂 `E:\XDev\manifests\`銆?
### T-D1 vscode
- [ ] Win zip / mac dmg / linux tar.gz锛堝畼鏂?/sha/download 绔偣锛夛紱form [gui,cli]锛沢ui_binary bin/code.exe锛沴atest + 1 鏃х増

### T-D2 jetbrains-idea-community
- [ ] Win exe锛坕nstall_kind executable + `/S /CONFIG=... /D={{install}}`锛? mac dmg / linux tar.gz锛沠orm [gui,cli]锛沢ui_binary 鏍稿疄锛坆in/idea64.exe / bin/idea.sh锛夛紱鏃?depends锛堣嚜甯?JBR锛?
### T-D3 cursor + zed
- [ ] cursor锛氬畼鏂?JSON API URL锛沠orm [gui,cli]锛涢潤榛樺弬鏁版湭鏂囨。鍖?鈫?zip/瀹樻柟鑴氭湰浼樺厛
- [ ] zed锛欸itHub releases zip/tar.gz/dmg锛沠orm [gui,cli]锛沢ui_binary 鏍稿疄锛坺ed.exe / zed锛?
### T-D4 claude-code
- [ ] native 浼樺厛锛坕nstall.sh/ps1锛? npm `@anthropic-ai/claude-code`锛沠orm [cli,tui]锛沝epends node鈮?2@npm

### T-D5 opencode
- [ ] CLI锛坣ative/npm `opencode-ai`锛? Desktop App锛沠orm [gui,cli,tui]锛沢ui_binary 妗岄潰 app 甯冨眬鏍稿疄锛沶pm 鍏?Node

### T-D6 codex
- [ ] CLI native锛坕nstall.sh/ps1/winget锛? npm `@openai/codex`锛沠orm [gui,cli,tui]锛坓ui=ChatGPT 妗岄潰搴旂敤锛屾敞閲婅鏄庯級锛沝epends node鈮?6@npm

### T-D7 copilot-cli
- [ ] npm `@github/copilot` + native锛坓h.io/copilot-install锛夛紱form [cli,tui]锛沝epends node鈮?2@npm

## Phase E 鈥?闆嗘垚楠岃瘉

### T-E1 鍏ㄩ噺楠岃瘉涓庢墜鍔ㄥ満鏅?- [ ] 鍏ㄩ噺 `go test ./... -short` 閫氳繃锛堟棤 FAIL锛?- [ ] `wails build` 浜у嚭 xdev-gui.exe
- [ ] 鎵嬪姩鍦烘櫙閫氳繃锛歷scode 瀹夎+鍚姩 / claude-code 渚濊禆鎻愮ず锛坣ode 缂?婊¤冻鍒囨崲锛? codex 涓夊舰鎬佸窘鏍?/ 澶嶅埗鍛戒护
- [ ] spec/plan/task/checklist 鐘舵€佹洿鏂颁负瀹屾垚

---

## 鎵ц璁板綍

| 鏃ユ湡 | Task | 楠屾敹缁撴灉 | 澶囨敞 |
|------|------|----------|------|
| 2026-08-02 | T-A1..A6 | 通过（6 包测试全绿；L3 审查 2 轮修复后 APPROVE） | B1 完成 |
| 2026-08-02 | T-A1..A6 | 通过（5 包全绿；L3 2 轮修复后 APPROVE） | B1+B2 完成 |
| 2026-08-03 | T-B1..B2 | 通过（CheckToolDependencies 9 测试全绿；依赖提示 UI e2e 验证通过） | B3 完成 |
| 2026-08-03 | T-C1..C2 | 通过（LaunchTool/ToolCommand 12 测试全绿；形态徽标/启动/复制命令 UI，e2e 18/18 全绿） | Phase C 完成 |
| 2026-08-03 | T-D1..D7 | 通过（8 manifest 全部产出：vscode/jetbrains-idea-community/cursor/zed/claude-code/opencode/codex/copilot-cli；URL HEAD 200 + Windows 实测 SHA 回填 + 同步运行时；manifest/remote/cli 测试全绿） | Phase D 完成 |
| 2026-08-03 | T-E1 | 通过（全量 go test 26 包全绿；wails build 产出 xdev-gui.exe；CLI 重建后 8 新工具注册；codex 真实安装+运行验证；vscode 真实安装+启动验证——修复 strip:0/Code.exe 缺陷；依赖提示/徽标/复制命令 e2e 覆盖） | 全部完成 |

---

**瀹℃壒璇锋眰**锛氳瀹℃壒鏈?checklist銆傝幏鎵瑰悗杩涘叆 **LOOP 瀹炴柦**锛堣 loop.md锛夛細鎸夋壒娆″疄鏂?鈫?鑷姩楠岃瘉 鈫?瀹℃煡 鈫?澶辫触鍥為€€閲嶈瘯锛岀洿鍒板叏閮ㄦ敹鏁涖€?








