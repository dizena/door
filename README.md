# Door

<p align="center">
  <strong>超轻量级原生 Linux 服务器运维面板（Go + SQLite + Vue 3 单二进制）</strong>
</p>

<p align="center">
  不强依赖 Docker · 零 CGO 静态编译 · 仿宝塔交互体验 · 适配 256MB/512MB 低配 VPS
</p>

---

## 项目简介

**Door** 是一款采用 **Go 1.26 + 纯 Go SQLite + 内嵌 Vue 3 SPA** 构建的开源轻量级 Linux 运维面板。参考宝塔面板的操作体验，但摒弃了沉重的运行时与依赖，以单一可执行文件交付（Linux `amd64` 约 15MB / `arm64` 约 14MB），直接以宿主机原生方式隔离托管多语言应用、数据库与 Nginx 服务。

### 核心特性

- **单文件极简部署**：前端静态资源通过 `//go:embed` 编译进 Go 二进制，数据库采用零 CGO 的 `modernc.org/sqlite`（WAL 模式），支持跨平台 `CGO_ENABLED=0` 静态编译。
- **低内存友好**：支持配置 Go 软内存上限（`memory_limit_mb`，默认 `128 MiB`），不依赖 Docker Daemon 即可原生部署管理软件，节省大量基础内存开销。
- **隔离目录规范**：所有服务与运行数据统一规范在 `/door` 目录下，互不污染系统全局环境：
  - `/door/server`：面板数据（`data/door.db`）、备份归档（`backup/`）、多语言运行时（`java/`、`python/`、`go/`）与日志（`logs/`）
  - `/door/database`：MySQL、PostgreSQL、Redis、MongoDB 隔离安装目录、数据与配置
  - `/door/project`：托管的 Go / Java / Python 项目及站点根目录
  - `/door/nginx`：原生 Nginx 二进制、主配置、`conf/vhost/*.conf`、SSL 证书与访问日志
- **可拖拽动态二级菜单**：内置仿宝塔左侧导航栏，支持在「面板设置 → 菜单配置」中**拖拽调整**菜单排序与一级/二级归属（同级重排、二级跨组移动、二级升一级、无子菜单的一级降为二级）、自定义名称与图标、显隐控制及一键恢复默认。
- **跨平台本地沙箱开发**：在 macOS 等非 Linux 开发环境下自动启用本地沙箱与模拟 Systemd 驱动（`./.door-dev/`），无需 `root` 权限即可完整调试前后端功能。

---

## 功能概览

| 模块 | 核心能力 |
| --- | --- |
| **仪表盘与监控** | 实时采集 CPU、负载、内存、磁盘、网络流量与面板自身内存占用，展示 `/door/*` 目录状态 |
| **Nginx 与网站** | 原生 Nginx 启停与平滑重载；反向代理/静态站点管理；每次写入自动执行 `nginx -t` 预检，失败自动回滚 |
| **SSL 证书** | 支持 **Let's Encrypt**（`go-acme/lego` HTTP-01 自动签发）、Go 原生 ECDSA 自签名测试证书、手动上传 PEM 证书 |
| **多语言项目部署** | 托管 **Go**（二进制）、**Java**（多版本 JDK）、**Python**（独立 `venv`），自动生成降权 Systemd Unit（`User=www`）与日志查看 |
| **原生数据库管理** | 支持 **MySQL / PostgreSQL / Redis / MongoDB** 隔离部署、随机强密码生成、配置文件在线编辑、建库/建用户及备份导出 |
| **Web SSH 终端** | 基于 `creack/pty` + WebSocket + `xterm.js` 的网页交互式终端，支持窗口大小自适应与同源校验 |
| **任务队列与审计** | 受控 Goroutine Worker 池执行安装等耗时任务，支持 SQLite 状态持久化、SSE 实时日志流与操作审计日志 |
| **安全基线** | 默认管理员首次登录强制修改初始密码；全程参数数组调用（杜绝 `sh -c` 注入）；`os.OpenRoot` 目录隔离防路径穿越 |

---

## 快速开始

### 1. 默认访问信息

- **默认监听端口**：`12345`（`http://<服务器IP>:12345`）
- **默认管理员账号**：`admin`
- **默认初始密码**：`12345678`

> **安全机制**：使用默认账号登录后，系统会强制拦截所有面板操作并跳转至改密页面，必须修改为至少 8 位的高强度密码后方可进入面板。

---

### 2. Linux 生产环境安装

在项目根目录完成构建后，使用安装脚本一键初始化 `/door/{server,database,project,nginx}` 并注册 `systemd` 服务：

```bash
# 1. 构建前端与 Linux 二进制
pnpm --dir web install
pnpm --dir web build
make build-linux

# 2. 部署到目标 Linux 服务器（需 root 权限）
sudo bash scripts/install.sh
```

常用运维命令：

```bash
systemctl status door
systemctl restart door
journalctl -u door -f
```

---

### 3. 本地开发调试（macOS / Linux）

前置要求：**Go 1.26+**、**Node.js & pnpm**。

```bash
# 安装并构建前端静态资源（嵌入 Go 二进制）
pnpm --dir web install
pnpm --dir web build

# 以本地沙箱模式启动面板（自动映射 /door/* 至 ./.door-dev/）
go run ./cmd/door serve --dev --addr :12345
```

如需前后端热更新联调：

```bash
# 终端 1：启动 Go 后端 API（:12345）
go run ./cmd/door serve --dev --addr :12345

# 终端 2：启动 Vite 前端开发服务器（:5173，自动代理 /api 至 :12345）
pnpm --dir web dev
```

启动参数说明：

| 参数 | 环境变量 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `--addr` | `DOOR_ADDR` | `:12345` | HTTP 服务监听地址与端口 |
| `--dev` | `DOOR_DEV` | macOS 默认 `true` | 启用本地沙箱与 Mock 系统驱动 |
| `--dev-root` | `DOOR_DEV_ROOT` | `.door-dev` | `--dev` 模式下的本地沙箱根目录 |
| `--memory-limit-mb` | `DOOR_MEMORY_LIMIT_MB` | `128` | Go GC 软内存上限（MiB，`0` 为不限制） |
| `--log-level` | `DOOR_LOG_LEVEL` | `info` | 日志级别（`debug` / `info` / `warn` / `error`） |

---

## 构建与测试

```bash
# 静态检查与单元测试（含竞态检测）
go vet ./...
go test -race ./...

# 编译当前平台二进制
make build

# 交叉编译 Linux 单文件（amd64 & arm64，零 CGO）
make build-linux
```

---

## 目录结构

```text
door/
├── AGENTS.md                   # 架构规范、安全红线与开发协作指南
├── Makefile                    # 统一构建、测试与交叉编译入口
├── cmd/door/main.go            # 程序入口、CLI 参数、优雅退出
├── internal/
│   ├── app/                    # 多语言项目（Go/Java/Python）与运行环境托管
│   ├── auth/                   # 登录鉴权、强制改密、Session 与审计日志
│   ├── config/                 # 配置加载与 /door/* 目录解析
│   ├── database/               # MySQL / PostgreSQL / Redis / MongoDB 原生管理
│   ├── monitor/                # 主机系统资源指标采集
│   ├── nginx/                  # Nginx 服务管理、vhost 生成、nginx -t 校验与 SSL
│   ├── settings/               # 面板基础设置与可拖拽二级菜单树
│   ├── store/                  # 纯 Go SQLite 连接与嵌入式 SQL 迁移
│   ├── sys/                    # OS 抽象驱动（Linux Systemd & 本地沙箱 Mock）
│   ├── task/                   # 异步任务 Worker 池与实时日志流
│   ├── terminal/               # Web PTY + WebSocket 终端
│   └── transport/http/         # REST API、SSE、鉴权中间件与静态托管
├── scripts/install.sh          # Linux 一键初始化与 Systemd 安装脚本
└── web/                        # Vue 3 + Vite + TypeScript + Tailwind CSS 前端
```

---

## 技术栈

- **后端**：Go 1.26（标准库 `net/http`、`log/slog`、`os.OpenRoot`、`text/template`、`embed`）
- **数据库**：`modernc.org/sqlite`（Pure Go SQLite）
- **核心库**：`shirou/gopsutil/v4`、`go-acme/lego/v4`、`creack/pty`、`gorilla/websocket`、`golang.org/x/crypto`
- **前端**：Vue 3、Vue Router、TypeScript、Tailwind CSS v4、`@xterm/xterm`、`lucide-vue-next`

---

## 开发规范与安全声明

欢迎提交 Issue 与 Pull Request。参与代码贡献前请阅读 [AGENTS.md](file:///Users/andy/code/door/AGENTS.md)，严格遵守以下安全底线：

1. 禁止使用 `sh -c` 拼接用户输入执行外部命令。
2. 所有文件写入操作必须限制在受控根目录内（`sys.SafeJoin` / `sys.WriteFileRoot`）。
3. 业务进程（Nginx、数据库及用户项目）在 Linux 下均须降权至无登录权限的系统账号运行。
4. 生产部署后请立即修改默认管理员密码，并配合防火墙或反向代理 HTTPS 加固面板访问。
