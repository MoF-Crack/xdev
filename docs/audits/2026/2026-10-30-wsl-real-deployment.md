# WSL 真实部署记录：Go 1.27.3 / Node 26.5.0 / Python 3.14.6（2026-10-30）

- 部署机：WSL2 Ubuntu 26.04.1（`xinyan`），真实 HOME `/home/xinyan`
- 使用二进制：仓库 `1.0.0-alpha.2` 构建产物 `~/xdev-src/build/bin/xdev-linux-full`（安装到 `~/.local/bin/xdev`）
- xdev home：`~/.local/share/xdev`（XDG 默认，非 `~/.xdev`）

## 一、部署步骤（实际执行）

```bash
# 0) 备份（可回滚）
cp -a ~/.bashrc ~/.profile ~/.xdev-deploy-backup-20261030-222759/
install -Dm755 ~/xdev-src/build/bin/xdev-linux-full ~/.local/bin/xdev

# 1) 初始化（写入 ~/.bashrc + ~/.profile，创建 ~/.local/share/xdev）
xdev init --yes

# 2) 安装 xdev 自身到 install 树并创建 shims/xdev
xdev self install

# 3) 三个工具
xdev install go@1.27.3      --yes --no-path-fix   # 阿里云镜像，8.9 s
xdev install node@latest    --yes --no-path-fix   # npmmirror，16.2 s（26.5.0）
xdev install python@latest  --yes --no-path-fix   # 3.14.6，见下方镜像说明
```

## 二、Python 镜像问题与处置（重要）

`manifests/python.yaml` 的 3.14.6 Linux `mirror_url` 指向
`https://mirrors.tuna.tsinghua.edu.cn/github-release/astral-sh/python-build-standalone/...`，
实测 **404**（该镜像站没有此路径）；官方 GitHub release 资产在 WSL 内被限速到 ~9 KB/s 且多次 `unexpected EOF`
（3 次重试 × 90 s 后失败，127 MB 无法完成）。

可用镜像：**兰州大学 `https://mirrors.lzu.edu.cn/github-release/astral-sh/python-build-standalone/`**
（实测 22 MB/s，1.5 s 下 32 MB）。

处置：用 LZU 镜像把归档预置到 xdev 的下载缓存，再由 `xdev install python@latest` 走 cache hit 完成校验/解包/激活：

```bash
curl -fL --retry 5 -o ~/.local/share/xdev/cache/python-3.14.6.tar.gz.part \
  "https://mirrors.lzu.edu.cn/github-release/astral-sh/python-build-standalone/20260728/cpython-3.14.6%2B20260728-x86_64-unknown-linux-gnu-install_only.tar.gz"
mv ...part ~/.local/share/xdev/cache/python-3.14.6.tar.gz
xdev install python@latest --yes --no-path-fix
```

归档：127,362,680 B；sha256 `b1ff3bb3ca58e2af9f1cb2be63cd9ef00a6f60ed50bba77bab189a9a3df13675`（TOFU 记录）。
**建议**：把 `manifests/python.yaml` 中 Linux 各版本的 `mirror_url` 改为 LZU 镜像（或增加 `mirror_urls` 列表），否则国内网络下 Python 安装基本不可用。

## 三、版本与环境验证（`bash -lc` 新登录 shell）

| 工具 | 命令 | 结果 |
|---|---|---|
| Go | `go version` | `go version go1.27.3 linux/amd64`（shim：`~/.local/share/xdev/shims/go`） |
| Node | `node --version` | `v26.5.0` |
| npm | `npm --version` | `11.17.0`（`npm config get prefix` = node 安装根） |
| Python | `python --version` | `Python 3.14.6` |
| pip | `pip --version` | `pip 26.1.2`（来自 python 安装树 `.../python/3.14.6/lib/python3.14/site-packages/pip`） |

内部路径 / 注入变量（子进程实测）：

```
GOROOT=/home/xinyan/.local/share/xdev/install/go/1.27.3/go
GOPATH=/home/xinyan/.local/share/xdev/install/go/gopath
node exec=/home/xinyan/.local/share/xdev/install/node/26.5.0/bin/node
npm prefix=/home/xinyan/.local/share/xdev/install/node/26.5.0
python prefix=/home/xinyan/.local/share/xdev/install/python/3.14.6
PYTHONUSERBASE=.../python/3.14.6/.userbase
PIP_CACHE_DIR=.../python/3.14.6/.pip-cache
```

## 四、可用性冒烟测试

| 测试 | 结果 |
|---|---|
| `go mod init` + `go build` 一个使用 `encoding/json` 的程序 | ✅ 输出 `{"hello":"xdev"}` |
| `node -e 'console.log(process.version, os.arch())'` | ✅ `v26.5.0 x64` |
| `python -c 'import ssl,sqlite3,zlib,ctypes,hashlib,json'` | ✅ `3.14.6 OpenSSL 3.5.7` |
| `python -m pip download six` | ✅ `six-1.17.0-py2.py3-none-any.whl`（PyPI 可达） |
| `npm view cowsay version` | ✅ `1.6.0`（registry.npmjs.org 可达） |
| `xdev doctor`（登录 shell） | ✅ `All checks passed`（含 login shell rc / shims / manifest） |
| `xdev list` / `current` / `which` | ✅ go/node/python/xdev 均 ★ 激活 |

## 五、磁盘与回滚

- 安装树：go 283 MB、node 225 MB、python 407 MB、xdev 17 MB；下载缓存 221 MB。
- 回滚：删除 `~/.local/share/xdev`、`~/.local/bin/xdev`，并用备份恢复 `~/.bashrc` / `~/.profile`：
  `cp -a ~/.xdev-deploy-backup-20261030-222759/.bashrc ~/.bashrc`（`.profile` 同）。

## 六、部署后目录

```
~/.local/share/xdev/
├── config.yaml                 (0600)
├── install/{go/1.27.3,node/26.5.0,python/3.14.6,xdev/1.0.0-alpha.2}
├── current/{go,node,python,xdev} -> install/...
├── shims/{go,gofmt,godoc,node,npm,npx,corepack,python,python3,pip,pip3,xdev}
└── cache/{go-1.27.3.tar.gz,node-26.5.0.tar.xz,python-3.14.6.tar.gz}
```
