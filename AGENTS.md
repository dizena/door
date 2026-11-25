# AGENTS.md — Door 轻量级 Linux 运维面板

> 本文档为 **Door** 项目（基于 Go + 原生软件托管 + SQLite + 内嵌静态前端的超轻量类宝塔运维面板）的架构规范与 AI/开发者协作指南，遵循 **Go 1.24+ / 1.26** 现代工程最佳实践。

---

## 1. 项目定位与核心目标

- **目标**：打造单二进制文件、极致低内存占用（常驻内存目标 **15MB–40MB**，适配 256MB/512MB 低配 VPS）、不强依赖 Docker 的原生 Linux 服务器运维面板，交互与功能设计参考宝塔面板。
- **核心目录规范（生产默认路径，支持在面板设置中调整，macOS `--dev` 模式自动映射到 `./.door-dev/` 沙箱目录）**：
  - `/door/server`：面板自身运行数据、SQLite 数据库、备份与多语言运行环境
    - `/door/server/data`（SQLite `door.db`、密钥、面板运行态）
    - `/door/server/backup`（数据库与站点备份归档）
    - `/door/server/java`（多版本 JDK 运行时，如 `jdk-17`、`jdk-21`）
    - `/door/server/python`（独立 Python 解释器与运行时）
    - `/door/server/logs`（面板与应用运行日志）
  - `/door/database`：原生数据库隔离安装目录、数据目录与配置
    - `/door/database/mysql`、`/door/database/postgresql`、`/door/database/redis`、`/door/database/mongodb`
  - `/door/project`：托管的多语言后端项目与站点根目录（Go、Java、Python 项目及 `venv`）
  - `/door/nginx`：原生 Nginx 二进制、主配置、`conf/vhost/*.conf`、SSL 证书与站点日志
    - `/door/nginx/sbin`、`/door/nginx/conf/vhost`、`/door/nginx/ssl`、`/door/nginx/logs`
- **核心能力**：
  1. **系统监控与终端**：CPU / 内存 / 磁盘 / 网络实时状态、Web SSH / 实时日志流。
  2. **多语言项目部署**：托管 Go（原生二进制）、Java（多版本 JDK）、Python（独立 `venv` + `gunicorn`/`uvicorn`），底层由 Systemd 守护进程。
  3. **数据库原生安装与管理**：支持 MySQL、PostgreSQL、Redis、MongoDB 的隔离目录部署、启停、配置修改、建库/建用户及备份导出。
  4. **Nginx 与 SSL 管理**：预编译/原生 Nginx 管理、站点反向代理可视化配置（`nginx -t` 校验 + 平滑 `reload`）、Let's Encrypt 免费证书自动签发与续期（`go-acme/lego`）。
  5. **面板设置与可拖拽菜单系统（仿宝塔左侧导航 + 设置中心）**：
     - **面板自身配置（`settings` 表）**：面板名称、监听地址/端口、安全入口路径、默认目录配置（`/door/server`、`/door/database`、`/door/project`、`/door/nginx`）、Go 堆内存上限（`GOMEMLIMIT`）、Session 过期时间等。
     - **二级动态菜单配置（`menus` 表）**：
       - 严格支持 **一级菜单 + 二级菜单** 两级树形结构（最大深度为 2）。
       - 默认一级/二级菜单示例：
         - `仪表盘`（一级）
         - `Nginx 与网站`（一级）→ `站点管理`、`SSL 证书`、`Nginx 服务`
         - `项目部署`（一级）→ `Go 项目`、`Java 项目`、`Python 项目`、`运行环境`
         - `数据库`（一级）→ `MySQL`、`MongoDB`、`Redis`、`PostgreSQL`
         - `系统监控与终端`（一级）→ `任务队列`、`Web 终端`、`审计日志`
         - `面板设置`（一级）→ `基础设置`、`菜单配置`、`安全设置`
       - **拖拽交互与后端校验**：前端设置页面支持**拖拽移动**，可调整同级排序、跨一级菜单移动二级菜单归属、将二级菜单升为一级菜单、将无子菜单的一级菜单降为其他一级菜单的二级菜单；同时支持自定义名称/图标、显示/隐藏开关与“恢复默认菜单”，由后端事务持久化到 SQLite 并校验层级深度、唯一 `key` 与无环约束。
- **跨平台开发策略**：
  - 开发机为 **macOS (`darwin/arm64`)**，生产目标为 **Linux (`linux/amd64`, `linux/arm64`)**。
  - 所有系统底层操作（Systemd、用户/权限、`apt`/`dnf`、服务启停）必须通过 `internal/sys` 抽象接口实现，提供 **Linux 原生实现** 与 **本地 Mock/Dry-Run 实现**（macOS 开发与单元测试默认走本地沙箱目录与模拟驱动，无需 root 即可完整调试）。

---

## 2. 常用命令（Build / Test / Lint）

> 当前环境：`Go 1.26.5`、`Node / pnpm`、`CGO_ENABLED=0` 纯 Go 静态编译。

```bash
# 运行全部单元测试（含竞态检测，开发阶段必跑）
go test -race ./...

# 静态检查与格式化
gofmt -w .
go vet ./...

# 本地开发运行（macOS 下自动启用本地沙箱 / Mock 系统驱动）
go run ./cmd/door serve --dev --addr :8080

# 交叉编译 Linux 生产单文件（零 CGO 依赖）
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/door-linux-amd64 ./cmd/door
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o bin/door-linux-arm64 ./cmd/door

# 前端构建并嵌入 Go 二进制（web/ 目录就绪后）
pnpm --dir web install --frozen-lockfile && pnpm --dir web build
```

---

## 3. 现代 Go 目录结构规范

采用现代精简布局（避免无意义的 `pkg/` 分层，以领域驱动组织 `internal/`）：

```text
door/
├── AGENTS.md                   # 本规范文档
├── Makefile                    # 统一构建、测试、交叉编译入口
├── go.mod / go.sum             # Go 模块定义（目标 Go 1.26）
├── cmd/
│   └── door/
│       └── main.go             # 程序入口：CLI 子命令、信号监听、优雅退出
├── internal/
│   ├── config/                 # 配置加载（Flag + Env + YAML，默认值与校验）
│   ├── store/                  # SQLite 持久化层（modernc.org/sqlite + WAL + 嵌入迁移）
│   │   └── migrations/         # 版本化 SQL 迁移脚本（//go:embed）
│   ├── sys/                    # OS 抽象层（Systemd、文件系统、进程、系统用户、包下载解压）
│   │   ├── driver.go           # 统一接口定义（ServiceManager, FileOps, UserManager）
│   │   ├── linux_systemd.go    # Linux 原生 Systemd & 降权执行实现
│   │   └── mock_local.go       # macOS / 测试用本地沙箱实现
│   ├── task/                   # 异步任务队列（Goroutine Worker Pool + SQLite 状态 + 实时日志流）
│   ├── monitor/                # 系统指标采集（shirou/gopsutil）
│   ├── auth/                   # 登录鉴权、Argon2id/Bcrypt 密码、Session/JWT、安全审计
│   ├── settings/               # 面板自身配置与二级动态菜单树（拖拽排序、一级/二级归属持久化）
│   ├── app/                    # 多语言应用托管领域（Go / Java / Python 运行时与进程管理）
│   ├── database/               # 数据库管理领域（MySQL / PgSQL / Redis / MongoDB）
│   ├── nginx/                  # Nginx 安装、vhost 模板渲染、nginx -t 校验、SSL（lego）
│   └── transport/
│       └── http/               # REST API + WebSocket（Go 1.22+ stdlib ServeMux、中间件、SSE）
├── web/                        # 前端 SPA（Vue 3 / Vite / TS / Tailwind）
│   ├── embed.go                # //go:embed dist/* 静态资源内嵌
│   └── ...
└── scripts/                    # 安装脚本、交叉编译与环境初始化脚本
```

---

## 4. Go 编码规范与现代惯用法

### 4.1 语言与标准库优先（Go 1.22–1.26）
- **HTTP 路由**：优先使用标准库 `net/http` 增强型 `ServeMux`（如 `mux.HandleFunc("GET /api/v1/sites/{id}", h.Get)` 配合 `r.PathValue("id")`）或极简中间件，控制依赖数量与二进制体积。
- **结构化日志**：全项目统一使用标准库 `log/slog`，严禁在业务代码中直接使用 `fmt.Println` 或全局 `log.Printf`。日志必须携带上下文关键字段（如 `task_id`、`site`、`service`），禁止打印密码、私钥、Token。
- **错误处理**：
  - 使用 `fmt.Errorf("action description: %w", err)` 保留错误链；判断错误统一用 `errors.Is` / `errors.As`。
  - 定义清晰的领域哨兵错误（`ErrNotFound`、`ErrConflict`、`ErrInvalidInput`、`ErrUnauthorized`），HTTP 层统一映射为标准状态码与 JSON 错误体。
  - 严禁在 HTTP Handler 或后台任务中 `panic`，异步 Worker 必须包含 `recover()` 兜底并记录堆栈。
- **上下文与生命周期**：
  - 所有 I/O、数据库查询、外部命令执行函数的第一个参数必须是 `ctx context.Context`。
  - 进程启动使用 `signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)` 实现 HTTP 服务、任务队列与数据库连接池的优雅关闭（Graceful Shutdown）。
- **并发安全**：
  - 禁止无界启动 `go func()`（fire-and-forget）；长耗时任务（软件安装、备份、初始化）必须提交至受控的 `internal/task` Worker 池并持久化状态。
  - 优先通过 `channel` 或小粒度 `sync.Mutex` / `sync/atomic` 管理共享状态，所有测试需通过 `-race`。

### 4.2 数据持久化（Pure Go SQLite）
- 驱动选用 `modernc.org/sqlite`（零 CGO），保证 `CGO_ENABLED=0` 跨平台静态编译。
- 初始化必设 Pragma：
  ```sql
  PRAGMA journal_mode = WAL;
  PRAGMA busy_timeout = 5000;
  PRAGMA foreign_keys = ON;
  PRAGMA synchronous = NORMAL;
  ```
- 为避免 `SQLITE_BUSY`，写入连接池控制或分离读写连接，禁止长事务中嵌套外部命令或网络下载。
- 表结构变更全部通过 `internal/store/migrations/*.sql` 增量迁移，禁止手动改表或直接覆盖生产库。

### 4.3 内存与资源控制（低配 VPS 友好）
- 入口支持通过配置或 `runtime/debug.SetMemoryLimit` 控制 GC 目标内存上限（如在 256MB 机器上限制 Go 堆上限，防止 OOM）。
- 大文件下载、日志读取、数据库备份导出必须使用流式传输（`io.Copy` / 分块读取 / SSE 或 WebSocket 推送），严禁将整个压缩包或大日志文件一次性读入内存。

---

## 5. 安全红线（运维面板核心底线）

> 运维面板通常以 `root` 权限运行，任何安全漏洞都等同于整台服务器沦陷，开发时必须严格遵守以下红线：

1. **严禁 Shell 拼接注入**：
   - 禁止使用 `exec.Command("sh", "-c", userInput)` 拼接任何外部参数！
   - 一律使用 `exec.CommandContext(ctx, binaryPath, arg1, arg2, ...)` 参数数组形式调用。
   - 域名、项目名、数据库名、用户名、端口号等输入必须在入口做严格的白名单正则校验。
2. **路径穿越防护**：
   - 所有涉及文件读写、站点根目录、备份路径的操作，必须限定在受控根目录（`/door/project`、`/door/server`、`/door/database`、`/door/nginx`）内。
   - 优先使用 Go 1.24+ 标准库 `os.Root`（目录隔离文件操作）或严格的 `filepath.Clean` + 前缀边界校验，拒绝包含 `..` 或越界软链接的路径。
3. **最小权限与进程降权**：
   - 面板自身仅在操作 Systemd、80/443 端口、系统用户时使用高权限；由面板拉起的 Nginx、MySQL、用户 Java/Python/Go 业务进程，必须在 Systemd Unit 中配置 `User=` / `Group=` 降权至专用无登录权限用户（如 `www`、`mysql`、`redis`）。
4. **Nginx 配置安全**：
   - 每次通过 `text/template` 写入或更新 `vhost/*.conf` 前，先写入临时文件并校验路径与模板变量；生效前必须执行 `nginx -t` 校验，校验失败立即回滚，绝不直接 `reload`。
5. **敏感数据保护**：
   - 数据库 root 密码、面板登录密码、SSL 私钥等严禁明文记录在日志或错误消息中；面板密码使用 `argon2id` 或 `bcrypt` 哈希存储；下载外部安装包必须校验 SHA256 校验和。

---

## 6. 渐进式实现路线图（Phased Roadmap）

遵循“先骨架验证、再逐模块落地、每步可独立运行与测试”的节奏推进：

- **Phase 1：项目骨架与基础设施**
  - 初始化 `go.mod`（Go 1.26）、目录配置（`/door/server`、`/door/database`、`/door/project`、`/door/nginx`）、结构化日志（`slog`）、SQLite + 迁移机制、统一 HTTP Server 与健康检查、基础认证（首次启动生成随机初始密码）、面板设置与二级拖拽菜单 API、`sys.Driver` 接口（Linux Systemd + macOS 本地沙箱 Mock）。
- **Phase 2：异步任务引擎、系统监控与仿宝塔前端界面**
  - `internal/task` 任务队列（状态落库 + 实时日志流 SSE/WebSocket）；`internal/monitor` 主机 CPU/内存/磁盘/网络指标采集与接口；Vue 3 + Tailwind 仿宝塔左侧动态菜单布局、仪表盘与**设置中心（含菜单拖拽排序与一级/二级归属调整）**，并通过 `//go:embed` 内嵌。
- **Phase 3：多语言应用部署（Go / Java / Python）**
  - 运行时目录管理（`/door/server/java`、`/door/server/python`）、项目目录（`/door/project`）、Systemd Unit 模板生成、项目增删改查、启停/重启、实时日志查看。
- **Phase 4：Nginx 管理与 SSL 自动签发**
  - Nginx 原生部署（`/door/nginx`）与状态管理、站点与反向代理配置生成、`nginx -t` 预检与回滚、集成 `go-acme/lego` 实现 Let's Encrypt 证书申请与自动续期。
- **Phase 5：数据库原生安装与管理**
  - MySQL / PostgreSQL / Redis / MongoDB 隔离目录安装（`/door/database`）与初始化、降权服务托管、配置文件管理、建库/建用户/改密与定时备份导出。
- **Phase 6：Web SSH 终端、安全加固与发布流水线**
  - 基于 `creack/pty` + WebSocket 的网页终端、操作审计日志、Linux 一键安装/升级脚本、多架构发布打包。

---

## 7. 变更与验收准则

每次提交或完成一个阶段功能时，必须满足：
1. `go vet ./...` 零警告。
2. `go test -race ./...` 全部通过（核心业务逻辑、参数校验、Nginx 配置生成、Systemd Unit 生成、HTTP Handler 均有表驱动单测）。
3. 至少验证一次 Linux 交叉编译：`CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./cmd/door`。
4. 不留未使用的依赖、死代码或硬编码路径（路径必须由 `config` 统一注入，以兼容 macOS 本地沙箱与 Linux `/door` 目录）。
