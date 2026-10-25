# Windows 拦截未签名 exe 的处理方法

## 现象

双击 `xdev-1.0.0-alpha.1.exe` 时 Windows 弹窗拦截；选择「仍要运行」后无反应。
而 `xdev-0.4.0.exe` 双击不被拦截、可正常打开 GUI。

## 为什么 0.4.0 不被拦截、新版被拦截

**不是程序缺陷，是 Windows 对「新出现且未签名」的可执行文件的信任策略。**

- 两个 exe **都没有数字签名**（`Get-AuthenticodeSignature` 均为 NotSigned）
- 差异在于 **0.4.0 是旧文件**：它在你的机器上已被反复运行，Windows/SmartScreen
  与安全软件已建立「这个哈希是安全的」的本地信誉记录
- 新版是**全新哈希**，没有任何本地信誉，因此被当作未知程序拦截

这解释了为什么同一份代码、同样的构建流程，只是版本号变了就被拦截。

## 已验证：程序本身是正常的

在本机构建机上实测（同一份产物）：

```
build\bin\xdev.exe version
  → xdev 1.0.0-alpha.1 (built 2026-10-25T05:12:41Z)     退出码 0

Start-Process（GUI 模式）
  → PID 存活，MainWindowTitle = "XDev - Development Environment Manager"
    Responding = True
```

CLI、GUI 均正常。所以问题在运行环境的信任策略，不在二进制。

## 解除拦截的方法（按推荐顺序）

### 方法 1：解除单个文件的阻止属性

如果文件是从别处复制/下载来的，Windows 会给它打「来自其他计算机」标记：

```powershell
Unblock-File .\xdev-1.0.0-alpha.1.exe
```

或右键文件 → 属性 → 底部勾选「解除锁定」→ 确定。

> 本机构建产物通常没有该标记，但复制到其他机器/网络盘后可能被加上。

### 方法 2：加入 Windows Defender 排除项（需管理员）

```powershell
# 以管理员身份运行 PowerShell
Add-MpPreference -ExclusionPath "D:\Env\Android\ReverseTools\ReverseEng\xmdevenvmt\build\bin"
```

或：Windows 安全中心 → 病毒和威胁防护 → 管理设置 → 排除项 → 添加文件夹。

### 方法 3：检查第三方安全软件

如果装了 360 / 火绒 / 卡巴斯基等，它们对**未签名的新程序**通常更激进：

- 打开该软件的「信任区 / 白名单」，把 xdev 所在目录加入
- 或在拦截弹窗里选择「允许本次执行」并勾选「不再提醒」

### 方法 4：SmartScreen「更多信息 → 仍要运行」

如果弹的是蓝色 SmartScreen 窗口：

1. 点「更多信息」
2. 点「仍要运行」

> 注意：你说「允许运行后毫无反应」——如果是这种方式点了仍无反应，
> 请先用**命令行**验证一次，排除 GUI 自身问题：
>
> ```powershell
> cd D:\Env\Android\ReverseTools\ReverseEng\xmdevenvmt\build\bin
> .\xdev-1.0.0-alpha.1.exe version
> ```
>
> 若这条命令能打印版本号，说明二进制正常，只是 GUI 被安全软件拦了；
> 若这条命令也无任何输出且退出码非 0，请把 `$LASTEXITCODE` 的值告诉我。

### 方法 5（根治）：给发布产物做代码签名

上述方法都是逐机处理。要彻底避免，需要购买代码签名证书（OV/EV）：

```powershell
# 有证书后
signtool sign /f cert.pfx /p <password> /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 xdev-1.0.0-alpha.1.exe
```

EV 证书还能直接建立 SmartScreen 信誉，无需等待累积。

## 顺带说明：`version` 命令「无法打印版本」的排查

在排查中我发现一个**容易误判的现象**：

```powershell
# 方式 A：某些情况下 PowerShell 不回显
& .\xdev.exe version        # 可能看不到输出

# 方式 B：可靠
.\build\bin\xdev.exe version
cmd /c "xdev.exe version"
Start-Process .\xdev.exe -ArgumentList version -NoNewWindow -Wait -PassThru
```

如果你用方式 A 且看不到输出，请改用方式 B 复测，并检查 `$LASTEXITCODE`。
