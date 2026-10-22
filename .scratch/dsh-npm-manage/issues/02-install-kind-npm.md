# 02: manifest 新增 install_kind: npm

Status: done

manifest.go 增加 InstallKindNPM 常量；EffectiveKind 显式识别（沿用
executable/script 的「显式声明才执行」信任模型）。npm-kind 安装不下载归档，
由 npm 解析依赖并生成启动器。
