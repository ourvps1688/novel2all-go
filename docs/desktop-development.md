# Novel2ALL Desktop - 开发文档

**项目**: `D:\OHMYSTORY\novel2all-go\desktop\`
**作者**: novel2all-bot
**最后更新**: 2026-09-19 14:02 (每次任务完成更新)

> ⚠️ **自动化规则**: 任何 Phase 任务完成后**必须**立即更新本文档的"变更日志"章节 + 更新顶部"最后更新时间"。  
> 不允许"先 commit 等下再补"。Commit 完成 / CI 通过 / Phase 完成 = 立即更新文档。

---

## 1. 项目目标

将 novel2all-go 后端 + web-react 前端打包为 **Windows 单文件桌面应用** (~17 MB .exe)，让用户：

1. 在本地桌面创作小说 (章节 / 人物 / 关系 / 伏笔等)
2. 通过 Cloudflare Tunnel 域名访问后端 (`https://api.zxc.im`)
3. 桌面 app 内置 WebView2 (Windows 原生) 渲染 React UI
4. 自动从 GitHub Releases 更新版本
5. 用户数据本地 SQLite 存储 (离线可用)

**目标平台**: Windows 10/11 x86_64 (其他平台暂不考虑)

### 1.1 鉴权架构 (Phase 2)

桌面 app 用**账号 + 密码**登录，不做邮箱验证、第三方 OAuth 等：

| 子域名 | 用途 | 状态 |
|--------|------|------|
| **`auth.zxc.im`** | 用户鉴权 (login / refresh / logout) | 🟡 Phase 2 |
| **`api.zxc.im`** | 业务 API (projects / chapters / LLM) | ✅ Phase 1 |

**账号创建机制** (Phase 2 设计):
- ❌ 不做用户自助注册 (无邮箱验证，无忘记密码流程)
- ✅ **管理员手动创建账号** —— 现有 `admin/kent986611` 通过 admin panel 创建用户
- 桌面 app 用户拿管理员分配的账号密码登录
- 后端 SQLite `users` 表新增字段：`email` + `password_hash` (bcrypt) + `role` (admin/user)
- Phase 2 admin panel 端点: `POST /api/auth/users` (admin only) 创建用户
- Phase 2 用户登录端点: `POST /api/auth/login` 返 access_token (JWT 24h) + refresh_token (30d)

---

## 2. 技术栈

| 层 | 技术 | 版本 |
|----|------|------|
| 桌面框架 | **Wails v2** | v2.16.0 |
| 后端 | novel2all-go (复用) | main |
| 前端 | React + Vite + TypeScript | React 18 + Vite 5 |
| UI 样式 | 原生 CSS | — |
| 数据库 | SQLite (本地, Phase 2+) | — |
| 后端公网 | Cloudflare Tunnel | — |
| 自动更新 | GitHub Releases | — |
| 打包 | NSIS | — |
| CI/CD | GitHub Actions (windows-latest) | — |

**为什么不 Electron**: Wails 用 Go + 系统 WebView (Windows WebView2)，单 binary ~17 MB vs Electron 150+ MB
**为什么不 Tauri**: Tauri 用 Rust，跟现有 Go 后端集成需要 FFI，复杂度更高

---

## 3. 当前状态 (2026-09-19)

| Phase | 描述 | 状态 |
|-------|------|------|
| **0** | 部署 novel2all-go 后端到 192.168.3.106 | ✅ 完成 |
| **1.1** | Wails 项目初始化 (React + TypeScript template) | ✅ 完成 |
| **1.2** | app.go 后端代理 + App.tsx 登录/项目列表 UI | ✅ 完成 |
| **1.3** | GitHub Actions 自动 build + NSIS 安装脚本 | ✅ 完成 (CI #175 全绿) |
| **1.4** | 单实例锁 + 系统托盘 + 关窗隐藏 | ✅ 完成 |
| **2** | 账号体系 (JWT / 注册 / 登录) | 🟡 待启动 |
| **3** | 自动更新 (Wails pkg/updates) | 🟡 待启动 |
| **4** | 完整 NSIS 打包发布 | 🟡 待启动 |

---

## 4. 架构图

```
┌─────────────────────────────────────────────┐
│ Windows Desktop (Novel2ALL.exe, 17 MB)    │
│                                              │
│ ┌──────────────────────────────────────┐   │
│ │ Go Backend (Wails main + app.go)  │   │
│ │ - BackendURL()  → "https://api.zxc.im"  │   │
│ │ - HealthCheck() → GET /health           │   │
│ │ - Login()       → POST /api/auth/login   │   │
│ │ - ListProjects()→ GET /api/projects     │   │
│ │ - ListChapters()→ GET /api/projects/X/chapters │   │
│ │ - token 持久化: %APPDATA%\Novel2ALL\token │   │
│ └──────────────────────────────────────┘   │
│ ┌──────────────────────────────────────┐   │
│ │ React Frontend (WebView2)           │   │
│ │ - LoginPage (Phase 1: mock login)  │   │
│ │ - ProjectList (admin sees all)      │   │
│ │ - ChapterList                        │   │
│ └──────────────────────────────────────┘   │
│ ┌──────────────────────────────────────┐   │
│ │ Wails v2.16.0 runtime               │   │
│ │ - Bindings auto-generation            │   │
│ │ - Asset embed (frontend/dist)         │   │
│ └──────────────────────────────────────┘   │
└─────────────────────────────────────────────┘
                  ↓ (HTTPS REST API)
         ┌─────────────────────┐
         │ Cloudflare Edge      │
         │ (zxc.im domain)      │
         └─────────────────────┘
                  ↓ (Cloudflare Tunnel)
         ┌─────────────────────┐
         │ novel2all-auth tunnel│
         │ (ID: ce5d83fa-...)    │
         └─────────────────────┘
                  ↓
         ┌─────────────────────┐
         │ Ubuntu 24.04         │
         │ 192.168.3.106        │
         │ novel2all-go (port 8000) │
         └─────────────────────┘
                  ↓
         ┌─────────────────────┐
         │ SQLite /var/lib/...  │
         └─────────────────────┘
```

---

## 5. 项目结构

```
D:\OHMYSTORY\novel2all-go\desktop\
├── main.go                    # Wails app entry point
├── app.go                     # Go methods exposed to frontend
├── go.mod                     # Go dependencies
├── go.sum
├── wails.json                 # Wails config (productName: Novel2ALL)
├── frontend/
│   ├── src/
│   │   ├── App.tsx            # Main React component (login + project/chapter list)
│   │   ├── App.css            # Styles (sidebar + content + status bar)
│   │   ├── main.tsx           # React entry
│   │   └── assets/
│   ├── package.json
│   ├── vite.config.ts
│   ├── tsconfig.json
│   ├── wailsjs/               # Auto-generated Go bindings (TypeScript)
│   │   ├── go/
│   │   │   ├── main/
│   │   │   │   ├── App.ts     # Generated Go bindings
│   │   │   │   └── App.d.ts   # TypeScript type definitions
│   │   │   └── models.ts     # Generated TypeScript models
│   │   └── runtime/           # Wails JS runtime helpers
│   └── node_modules/          # (gitignored)
└── build/
    ├── appicon.png            # App icon (Windows .ico)
    └── windows/
        ├── icon.ico
        ├── info.json          # Windows manifest
        └── installer/
            └── project.nsi    # NSIS installer script (Phase 1.3)
```

---

## 6. 文件: app.go 详解

`app.go` 是 Wails 暴露给前端的所有 Go 方法。

### 6.1 类型定义

```go
type User struct {
    ID       int64  `json:"id"`
    Username string `json:"username"`
    Role     string `json:"role"`
    Email    string `json:"email,omitempty"`
}

type Project struct {
    ID, OwnerID         int64
    Name, Slug, Genre   string
    Description         string `json:",omitempty"`
    CreatedAt, UpdatedAt string `json:",omitempty"`
}

type Chapter struct {
    ID, ProjectID, Number int64
    Title, Filename string
    CharCount int
    CreatedAt, UpdatedAt string
}
```

### 6.2 核心方法

| 方法 | 后端调用 | 用途 |
|------|---------|------|
| `BackendURL()` | — | 返后端 base URL (前端显示) |
| `HealthCheck() (bool, string)` | GET /health | 启动时检测后端可达性 |
| `Login(user, pass) (*User, error)` | GET /version (Phase 1 mock) | 桌面登录 |
| `Logout()` | — | 清 token + 磁盘 |
| `IsLoggedIn() bool` | — | 检查 token 有效期 |
| `CurrentUser() *User` | — | 返当前 user |
| `ListProjects() []Project` | GET /api/projects | 列出项目 |
| `GetProject(id) *Project` | GET /api/projects/{id} | 单项目 |
| `ListChapters(projectID) []Chapter` | GET /api/projects/{id}/chapters | 列章节 |

### 6.3 Token 持久化

```go
// 路径: Windows %APPDATA%\novel2all-desktop\token
func (a *App) tokenPath() (string, error) {
    dir, err := os.UserConfigDir()  // %APPDATA% on Windows
    appDir := filepath.Join(dir, "novel2all-desktop")
    os.MkdirAll(appDir, 0o700)
    return filepath.Join(appDir, "token"), nil
}
```

### 6.4 后端地址

```go
backendURL: "https://api.zxc.im"  // Cloudflare Tunnel 域名
```

**不要改成 IP**（192.168.3.106:8000），Tunnel 已经把公网 HTTPS 转到本地 HTTP。

---

## 7. 文件: App.tsx 详解

`frontend/src/App.tsx` 是桌面 app 的 React 主组件。

### 7.1 启动流程

```typescript
useEffect(() => {
  (async () => {
    // 1. 拿后端 URL (Wails → Go → backendURL)
    const url = await BackendURL();
    
    // 2. 检测后端可达性
    const result = await HealthCheck();  // 返回 boolean | string
    
    // 3. 检查是否已登录 (从磁盘读 token)
    const ok = await IsLoggedIn();
  })();
}, []);
```

### 7.2 UI 状态机

```
未登录 → [LoginPage]
       ↓ Login()
登录  → [MainView: 项目列表 + 章节列表]
       ↓ Logout()
未登录
```

### 7.3 关键设计

- **不预存项目状态** — 每次进入页面都重新调 `ListProjects()`
- **token 持久化** — 由 Go 端管 (写在 `%APPDATA%`)
- **错误处理** — 所有 Wails 调用都用 try/catch

---

## 8. 文件: App.css 详解

UI 样式，三层布局：

```
┌─ Header (标题 + 用户) ─────────────────┐
├─ StatusBar (后端 URL + 健康检查 + 刷新) ┤
├─ Main (左 sidebar + 右 content) ───────┤
│ ┌─ Sidebar (项目列表) ┐ ┌─ Content (章节列表) ┐ │
│ │ 📚 项目            │ │ 📖 章节            │ │
│ │ ▸ My Novel (科幻) │ │ #1 第001章.md    │ │
│ └─────────────────┘ └─────────────────┘ │
├─ Footer (Phase 1 Novel2ALL) ──────────┤
```

颜色调色板：
- Primary: `#6366f1` (indigo-500)
- Background: `#fafafa`
- Card: `#fff`
- Border: `#e5e7eb`
- Text: `#1a1a1a`

---

## 9. 开发环境搭建

### 9.1 本机 (Windows 开发机) 必需工具

| 工具 | 版本 | 安装命令 |
|------|------|---------|
| **Go** | 1.27+ | `winget install GoLang.Go` |
| **Node.js** | 20+ LTS | `winget install OpenJS.NodeJS.LTS` 或下载 zip |
| **Wails CLI** | v2.16+ | `go install github.com/wailsapp/wails/v2/cmd/wails@latest` |
| **NSIS** | 3+ | `winget install NSIS.NSIS` |
| **WebView2** | 系统自带 (Win10/11) | 自动包含 |
| **Git** | 系统自带 | — |

### 9.2 PATH 设置

Wails 需要 Go + Node 都在 PATH：
```powershell
$env:Path = "C:\Program Files\Go\bin;C:\Program Files\nodejs;C:\Users\Administrator\go\bin;" + $env:Path
$env:GOPROXY = "https://goproxy.io,direct"
$env:GOSUMDB = "off"
```

### 9.3 中国用户 npm 加速

```powershell
npm config set registry https://registry.npmmirror.com
```

---

## 10. 开发命令

### 10.1 开发模式 (热重载)

```powershell
cd D:\OHMYSTORY\novel2all-go\desktop
wails dev
```

**预期输出**：
```
Wails CLI v2.16.0
Executing: go mod tidy
  Generating bindings: Done.
  Installing frontend dependencies: Done.
    > frontend@0.0.0 build
    > tsc && vite build
2026/09/19 XX:XX:XX  Application started.
```

WebView 窗口自动弹出。

### 10.2 生产 build (Windows .exe)

```powershell
wails build -platform windows/amd64 -clean
```

**产出**：`build/bin/Novel2ALL.exe` (~17 MB)

### 10.3 生产 build + NSIS installer

```powershell
wails build -platform windows/amd64 -clean -nsis
```

**产出**：`build/bin/Novel2ALL.exe` (NSIS wrapper)

---

## 11. 部署流程

### 11.1 Phase 0: 后端部署 (已完成 ✅)

**目标服务器**: `soap@192.168.3.106` (Ubuntu 24.04)
**部署脚本**: 通过 SSH + bash heredoc 执行

**部署内容**：
- `/opt/novel2all-go/` - Go 源码 (git clone)
- `/usr/local/bin/novel2all` - 编译后 binary (18 MB)
- `/var/lib/novel2all/novel2all.db` - SQLite DB
- `/etc/systemd/system/novel2all.service` - systemd unit
- 环境变量: `ADMIN_USER=admin`, `ADMIN_PASS=kent986611`, `DB_DSN`, `HTTP_PORT=8000`

**systemd 启动**：
```bash
sudo systemctl enable --now novel2all.service
```

### 11.2 Cloudflare Tunnel 配置

**Tunnel ID**: `ce5d83fa-3755-4c21-bf32-dff81173878b`
**Tunnel Name**: `novel2all-auth`

**Public Hostnames** (路由):
- `auth.zxc.im` → `http://localhost:8000` (Phase 0 配置)
- `api.zxc.im` → `http://localhost:8000` (Phase 1.2 加的)

**验证**:
```bash
curl -i https://api.zxc.im/health
# 期望: HTTP 200 + {"status":"ok",...}
```

---

## 12. 测试

### 12.1 启动时测试

- 启动 Novel2ALL.exe
- 应看到:
  1. 后端状态栏: `✅ 后端 OK` 或 `❌ 错误信息`
  2. 登录框 + 用户名/密码输入

### 12.2 登录测试 (Phase 1 mock)

- 任意用户名 + 密码登录
- 应进入主界面:
  - 顶部 user 显示
  - 项目列表 (空，admin 角色)
  - 章节列表 placeholder

### 12.3 错误场景

- **后端不可达**: WebView 显示 `❌ 后端 unreachable: ...`
- **Token 过期**: 自动重新登录
- **网络慢**: spinner 转圈

---

## 13. 已知问题 / 待办

### 13.1 已知 bug

| 问题 | 状态 | 备注 |
|------|------|------|
| Phase 1 mock 登录 (任何账号密码都过) | 待解决 | Phase 2 接入真实 JWT |
| ListProjects 需要 admin 角色 (Sprint V1.0.1 P0-B) | 待解决 | owner check 已加，但 API 端返回可能空 |
| 桌面 app 没有图标 (使用默认 Wails 图标) | 待解决 | Phase 1.3 加 .ico |
| 没有自动更新 | 待解决 | Phase 3 计划 |
| 没有系统托盘 | 待解决 | Phase 1.4 计划 |
| 没有单实例锁 | 待解决 | Phase 1.4 计划 |

### 13.2 下一步优先级

1. **Phase 1.3** - GitHub Actions + NSIS (1d)
2. **Phase 1.4** - 系统托盘 + 单实例 (1d)
3. **Phase 2** - 真实账号体系 (3d)
4. **Phase 3** - 自动更新 (1d)
5. **Phase 4** - NSIS 完整打包 (2d)

---

## 14. 调试

### 14.1 WebView 看不到元素

按 F12 打开 DevTools (Wails 自动启用 devtools).

### 14.2 Go 端日志

`app.go` 用 `runtime.LogInfo(ctx, ...)` 输出到 wails dev 终端。

### 14.3 后端日志

SSH 到 192.168.3.106：
```bash
sudo journalctl -u novel2all.service -f
```

### 14.4 网络诊断

```bash
# 本地到后端
curl http://localhost:8000/health  # 在 192.168.3.106

# 桌面 app 到后端 (Tunnel)
curl https://api.zxc.im/health
```

### 14.5 Wails 绑定重生成

如果改了 `app.go` 但前端没看到新方法，删 `frontend/wailsjs/` 重 build:
```bash
rm -rf frontend/wailsjs
wails dev
```

---

## 15. 配置文件清单

### 15.1 wails.json

```json
{
  "name": "Novel2ALL",                  // 产品名
  "outputfilename": "Novel2ALL",        // 输出 binary 名
  "frontend:install": "npm install",
  "frontend:build": "npm run build",
  "frontend:dev:watcher": "npm run dev",
  "frontend:dev:serverUrl": "auto",
  "author": {
    "name": "novel2all-bot",
    "email": "novel2all@ourvps1688.local"
  },
  "info": {
    "productName": "Novel2ALL",
    "companyName": "novel2all",
    "productVersion": "1.0.0"
  }
}
```

### 15.2 系统变量 (.env.development)

```ini
# frontend/.env.development (Phase 2+ 加入)
VITE_API_BASE=https://api.zxc.im
```

---

## 16. CI/CD (待实现)

### 16.1 GitHub Actions Workflow

文件路径: `.github/workflows/release.yml`

```yaml
name: Release
on:
  push:
    tags: ['v*']
jobs:
  build:
    runs-on: windows-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.27' }
      - uses: actions/setup-node@v4
        with: { node-version: '20' }
      - run: go install github.com/wailsapp/wails/v2/cmd/wails@latest
      - run: cd desktop && wails build -platform windows/amd64 -clean -nsis
      - uses: softprops/action-gh-release@v1
        with:
          files: desktop/build/bin/Novel2ALL.exe
```

---

## 17. 常见问题 (FAQ)

### Q1: WebView 显示空白?

1. 按 F12 打开 DevTools
2. 看 Console 错误
3. 大概率是 npm install 没跑 → `cd frontend && npm install`

### Q2: wails dev 一直卡 Installing frontend dependencies?

1. Ctrl+C 中断
2. 删 `frontend/node_modules` 和 `frontend/package-lock.json`
3. 重跑 `wails dev`

### Q3: 怎么更新后端地址?

改 `app.go` 的 `backendURL` 字段，重启 `wails dev`。

### Q4: NSIS installer 文件路径?

`build/bin/Novel2ALL.exe` (wails build -nsis 自动生成)
或 `desktop/build/bin/Novel2ALL.exe` (新版本)

### Q5: 怎么添加新 API 方法?

1. 在 `app.go` 加新方法 (返回 `(someType, error)`)
2. `wails dev` 会自动重新生成 TypeScript 绑定
3. 前端 import 绑定，直接调用

---

## 18. 参考资料

- **Wails 官方文档**: https://wails.io
- **Wails v2 GitHub**: https://github.com/wailsapp/wails
- **novel2all-go 后端文档**: `docs/p3-vendor-alignment-plan.md`
- **Cloudflare Tunnel**: https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/

---

## 19. 变更日志 (Change Log)

**约定**: 每次完成 Phase 任务后立即更新本节。
格式: `YYYY-MM-DD HH:MM | Phase X.Y | 描述 | commit hash`

### 2026-09-19

| 时间 | Phase | 描述 | Commit |
|------|-------|------|--------|
| 10:30 | Phase 0 | SSH 部署 novel2all-go 到 192.168.3.106, systemd 服务 active | (未 commit) |
| 11:30 | Phase 1.1 | Wails 项目初始化 (desktop-novel2all → desktop 重命名) | (未 commit) |
| 12:30 | Phase 1.2 | 写 app.go: BackendURL/HealthCheck/Login/Logout/Projects/Chapters 代理到 https://auth.zxc.im | (未 commit) |
| 12:55 | Phase 1.2 | wails dev 热重载成功, 修复 TS2488 (HealthCheck 返回 union type, 不是 tuple) | (未 commit) |
| 13:05 | Phase 1.2 | 加 api.zxc.im 第二个 Public Hostname, 后端地址改 https://api.zxc.im | (未 commit) |
| 13:10 | Phase 1.2 | 产品名 novel2all → Novel2ALL (wails.json + App.tsx + App.css 全栈) | (未 commit) |
| 13:20 | 文档 | 创建 docs/desktop-development.md (~600 行, 19 章节) | (未 commit) |
| 13:20 | 文档 | 补充 鉴权架构 (auth.zxc.im vs api.zxc.im, 管理员创建账号机制) | (未 commit) |
| 13:25 | Phase 1.3 | NSIS project.nsi (安装到 %LOCALAPPDATA%, 卸载, 快捷方式, 注册表清理) | (未 commit) |
| 13:25 | Phase 1.3 | GitHub Actions release.yml (push tag v* 自动 build + 上传 release) | (未 commit) |
| 13:30 | Phase 1.3 | git commit 4a0b8ec + push origin/main (23 files, +3996 lines) | 4a0b8ec |
| 13:31 | Phase 1.3 | CI #175 自动跑 (ci.yml, push:branches:main 触发) — 9/9 jobs 全绿 | (CI 通过) |
| 13:32 | 文档 | 同步登记 4a0b8ec commit + CI #175 全绿 (本节追加) | (本文档) |
| 13:36 | Phase 1.4 | 系统托盘 + 单实例锁 + HideWindowOnClose: internal/systray/tray.go + tray_net{,_unix}.go + main.go + app.go trayMenuRef 字段. 库: getlantern/systray v1.2.2 | (未 commit) |
| 13:36 | 文档 | Phase 1.4 状态表标 ✅ 完成 + changelog 登记 | (未 commit) |
| 13:48 | Phase 1.4 | git commit db1dd88 + push origin/main (8 files, +477 -10) | db1dd88 |
| 13:50 | Phase 1.4 | CI #176 (docs commit 6f1699d) 全绿 + CI #177 (Phase 1.4 code commit db1dd88) 全绿 | (CI 通过) |
| 13:58 | 文档 | 加 "AI 协作工作流" 章节 + Phase 1.4 标 ✅ | (本文档) |
| 14:02 | 文档 | CI #180 (75e91e4 AI workflow 文档 commit) 全绿 | (CI 通过) |

### 待办 (下一阶段)

- [ ] Phase 1.3: 应用图标 (PNG → .ico 转换) — 等待 UI 美化阶段
- [x] Phase 1.3: NSIS installer script ✅
- [x] Phase 1.3: GitHub Actions workflow ✅
- [x] Phase 1.4: 系统托盘 + 单实例锁 ✅ (CI #177 通过)
- [ ] Phase 2.1: 后端 /api/auth/users (admin create user) + /api/auth/login
- [ ] Phase 2.2: 后端 SQLite schema migration (users.email + password_hash)
- [ ] Phase 2.3: 桌面 app 真实 LoginPage 调 /api/auth/login
- [ ] Phase 3: Wails pkg/updates 自动更新 (检查 latest release, 下载, SHA256 校验)
- [ ] Phase 4: 完整 NSIS 打包 + GitHub Actions release + 自动更新通知

---

## 20. AI 协作工作流 (AI Collaboration Workflow)

> 本节给用户(开发者) + AI 助手(我) 协同工作的契约.

### 20.1 自动化规则 (AI 必须遵守)

每次完成 Phase 任务 / commit / push 后, AI 助手**必须立即**:

1. **代码改动**: `go vet` + `gofmt -s` + `go build` 全通过 (desktop/)
2. **commit + push**: 同一次操作的同一 commit
3. **CI 状态自动查** (`curl /actions/runs?per_page=1`):
   - success → 立即更新文档 "变更日志" 章节 + commit + push
   - failure → 修代码重新跑 (不绕过 CI)
5. **文档同步**: 每次任务完成**必须**立即更新本文档 (不允许"等下再补")
   - "状态" 列状态变化 (✅ 完成 / 🟡 进行中)
   - "变更日志" 章节加一行 (时间 + Phase + 描述 + commit hash)
   - 顶部"最后更新时间" 自动刷新

### 20.2 用户 (你) 的优化建议

**自动接受所有合理操作**:

WorkBuddy 有 sandbox rejection 机制, 我执行的某些操作 (ssh / scp / 大文件传输 / 写 SSH key) 可能触发 "User denied the permission request".

**优化方案** (你在 WorkBuddy 设置):
- WorkBuddy → 设置 → 信任权限 → "Auto-approve" (或类似选项)
- 白名单特定操作: `git push`, `ssh`, `scp`, `npm install`, `wails build`
- 设置默认 "Allow" 而非 "Ask"

这样我跑命令不会每次弹确认.

**推荐信任列表** (Phase 1 范围):
- `Bash`: git (push, commit, add), curl (GitHub API), ssh (到 192.168.3.106), scp
- `Edit / Write`: 仅对 `D:\OHMYSTORY\novel2all-go\` 子目录
- `Glob`: 无限制 (只读)

### 20.3 不需要 AI 等待的

- **CI 状态**: AI 自动查 (curl)
- **build / vet / gofmt**: AI 自动跑
- **commit / push**: AI 自动 (除非 force-push / 删分支, 这要你确认)
- **写文档**: AI 自动 (本文档 + 各模块 docs/)

### 20.4 需要你确认的

- **首次 SSH 凭证**: 你给过一次, AI 用 ~/.ssh/id_ed25519
- **密码 / secrets**: 你提供
- **force push / reset --hard**: 不可逆操作
- **删文件 / rm -rf**: 不可逆操作
- **外部服务付费**: Cloudflare Workers Paid / Apple Developer Account 等

### 20.5 CI 自动触发

- `push:branches:main` → ci.yml (lint + test + 6 build matrix + smoke)
- `push:tags:v*` → release.yml (Wails build + GitHub Release)

每次 commit push 都自动跑 CI, AI 主动 curl 查结果.

---

**文档结束** · 维护者: novel2all-bot · 下次更新: 下一任务完成后