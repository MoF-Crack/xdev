# 提取格式和资源预算

`Extract` 在目标目录旁新建独占 staging，校验结束后才发布；目标必须不存在或为空，不合并已有安装树。调用方须串行化同一目的地的发布。调用失败后不应删除目的地原有内容。

| 输入 | 解码方式 |
|---|---|
| ZIP、TAR（含 gzip、bzip2、xz、zstd） | Go 按成员读、校验完整名称并限额写入 |
| 原生 7z | Go `sevenzip` 读取单卷归档中的结构化完整成员名；按成员对象读取数据，不使用 `7z l -slt`、外部解码程序或自动打开相邻分卷。普通文件及目录支持，链接、reparse 和特殊文件拒绝。实际为 ZIP/TAR 的 `7z/exe/pkg` 输入走对应的原生读取。 |
| XAR | 需要能读 XAR 的 `bsdtar`（Windows/macOS 也识别系统的 `tar`）；转换为 stdout 上的 PAX 流，由同一受控写端提取，不让它自行写出成员。 |
| NSIS/Inno/SFX/其他安装器 | 提取模式不执行它们，也不会回退到外部直接写盘；不支持的容器报错。显式 `install_kind: executable/script` 是单独的安装器流程，未改变。 |
| macOS DMG | `hdiutil` 只读挂载；Go 计额复制顶层 `.app` 的文件与安全相对链接。不能把此路径当 `ditto` 全元数据克隆：显式拒绝已识别的资源分支、FinderInfo、分离签名属性。不承诺 ACL、所有者、扩展属性等保真。 |
| AppImage | 计额复制单文件、设置执行位，不运行文件。 |

本地操作者可设置以下**进程环境变量**。来源不能是未验证的归档字段；`Archive.Size` 是压缩体积，不代表展开预算。也可调用 `ExtractWithOptions(ctx, ..., budget)` 为受信任的个案设置限制。

| 变量 | 默认 |
|---|---:|
| `XDEV_EXTRACT_MAX_FILE_BYTES` | 1 GiB |
| `XDEV_EXTRACT_MAX_TOTAL_BYTES` | 10 GiB |
| `XDEV_EXTRACT_MAX_ENTRIES` | 200000 |
| `XDEV_EXTRACT_MAX_DEPTH` | 128 个有效路径分量（strip 前） |
| `XDEV_EXTRACT_TIMEOUT` | `30m`（Go duration） |
| `XDEV_EXTRACT_RESERVE_BYTES` | 64 MiB；`0` 明确关闭此检查 |

例如：受信任的归档确有一个 1.5 GiB 成员，可在本次进程设置 `XDEV_EXTRACT_MAX_FILE_BYTES=2147483648`，而不必增加累计预算。字节单位为十进制整数。无效设置报错。写入前检查声明大小，每次 `Write` 另查实际字节，超出声明/额度的块不会被交给文件系统。空间预留基于文件系统实时查询及至多 8 MiB 的本地记账，不能保证其他进程不抢占空间；这些预算也不是对解析器内存或阻塞内核 I/O 的 OS 级硬上限。
