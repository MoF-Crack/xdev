# 05: dsh manifest + GUI/前端暴露

Status: done

- `manifests/dsh.yaml`：development_tools 分类（opencode 同类）、shims [dsh]、
  form_factor [cli, tui]、depends node>=22 (install_path: npm)、
  versions 0.1.2-rc.1 / 0.1.1-rc.2（stable: false，真实发布日期）、
  archive 元数据条目（install_kind: npm，registry tgz URL + npmmirror）、
  remote npm pattern `^(\d+\.\d+\.\d+)(-(alpha|beta|rc)\.\d+)?$`。
- GUI：installWithOptions npm 提前分派（异步事件）；VersionInfo.Stable 透出；
  versionUnverified 对 npm-kind 豁免。
- 前端：VersionList 预发行徽标（stable===false 时）；i18n detail.prerelease
  en/zh 双语（parity 守卫）。

## Comments

- npm latest dist-tag 实测即 rc（0.1.2-rc.1），pattern 必须接受预发布，否则零版本可发现。
