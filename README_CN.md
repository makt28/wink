# Wink

极简、高性能、单文件自托管监控工具。

Uptime Kuma 的轻量替代方案 —— 零依赖、文件存储、单个可执行文件。

## 功能特性

- **单二进制文件** —— Go 后端 + 嵌入式前端，无任何运行时依赖
- **文件存储** —— JSON 配置和历史数据，无需数据库
- **HTTP / TCP / ICMP** 监控，支持自定义检测间隔
- **防抖机制** —— 连续失败达到阈值才触发告警，杜绝误报
- **重复告警** —— 故障后每 N 次失败重发通知，持续提醒
- **动态重试间隔** —— 故障时自动加速探测频率
- **Telegram & Webhook** 通知，可扩展的通知接口
- **通知备注** —— 为每个通知渠道添加备注标签，告警消息中清晰标识来源
- **通知渠道管理** —— 在设置页面直接编辑、测试、删除通知渠道
- **Telegram Chat ID 获取** —— 一键从 Bot API 获取可用聊天列表
- **精确通知目标** —— 每条监控可独立选择通知渠道
- **监控暂停/恢复** —— 临时禁用监控项，无需删除
- **分组监控列表** —— 按分组显示，支持折叠/展开
- **可用率追踪** —— 24 小时 / 7 天 / 30 天滑动窗口计算
- **心跳状态条** —— 每个监控项可视化展示近期探测结果
- **故障日志** —— 独立存储（`incidents.json`），自动保留 30 天并清理过期记录
- **时区设置** —— 首次启动自动检测系统时区，支持界面配置
- **SSO 单点登录** —— 支持反向代理 `Remote-User` 头认证
- **友好错误提示** —— 表单校验错误以弹窗方式显示，不中断操作
- **原子写入** —— 写入-同步-重命名，断电不丢数据
- **登录限速** —— 按 IP 锁定，防暴力破解
- **Session 过期** —— 自动清理过期会话
- **热重载** —— 增删改监控项无需重启
- **Web 设置** —— 在网页端配置系统参数、认证信息、分组和通知渠道
- **中英双语** —— 中文 / 英文界面一键切换
- **暗色模式** —— 明暗主题一键切换
- **健康检查** —— `GET /healthz` 供外部监控

## 快速开始

### Linux 一键安装（推荐）

系统要求：Linux（amd64/arm64）、systemd、curl。

```bash
curl -fsSL https://raw.githubusercontent.com/makt28/wink/main/install.sh | sudo bash
```

安装后使用 `wink` 命令管理服务：

```bash
sudo wink start       # 启动服务
sudo wink stop        # 停止服务
sudo wink restart     # 重启服务
sudo wink status      # 查看状态
sudo wink logs        # 查看日志（Ctrl+C 退出）
sudo wink update      # 更新到最新版本
sudo wink uninstall   # 卸载（保留数据文件）
sudo wink reinstall   # 重新下载并重启
```

数据文件存储在 `/opt/wink/`。

### 下载可执行文件

从 [Releases](https://github.com/makt28/wink/releases) 下载适合你平台的版本：

| 平台 | 文件 |
|---|---|
| Linux x86_64 | `wink-linux-amd64` |
| Linux ARM64 | `wink-linux-arm64` |
| macOS Apple Silicon | `wink-darwin-arm64` |
| Windows x86_64 | `wink-windows-amd64.exe` |

```bash
chmod +x wink-linux-amd64
./wink-linux-amd64
```

> **Termux (Android)：** 使用 `wink-linux-arm64` —— 可在 Termux 的 Linux 内核上直接运行。

### 从源码构建

需要 Go 1.24+ 和 Node.js（用于编译 Tailwind CSS）。

```bash
git clone https://github.com/makt28/wink.git
cd wink
npm install
make build
./wink
```

### 交叉编译

一次构建所有平台：

```bash
make cross
```

输出到 `dist/` 目录：

```
dist/wink-linux-amd64
dist/wink-linux-arm64
dist/wink-darwin-arm64
dist/wink-windows-amd64.exe
```

### 登录

浏览器打开 `http://localhost:8080`。

默认账号：**admin** / **123456**

首次登录后请在 **设置 > 认证** 中修改密码。

## 配置说明

完整 schema 参见 `config.json.example`，主要配置项：

| 配置段 | 说明 |
|---|---|
| `system` | 监听地址、检测间隔、历史数据上限、日志级别、时区（自动检测） |
| `auth` | 用户名、bcrypt 密码哈希、登录限速参数、SSO 开关 |
| `contact_groups` | 监控项的可视化分组 |
| `notifiers` | 通知渠道（Telegram、Webhook），支持备注标签 |
| `monitors` | 监控目标列表（HTTP、TCP、Ping） |

### 监控项字段

| 字段 | 说明 | 默认值 |
|---|---|---|
| `interval` | 检测间隔（秒） | 系统默认值 |
| `timeout` | 探测超时（秒） | 5 |
| `max_retries` | 标记故障前的失败次数 | 3 |
| `retry_interval` | 故障时加速检测间隔（0 = 使用普通间隔） | 0 |
| `reminder_interval` | 故障后每 N 次失败重发告警（0 = 不重发） | 0 |
| `ignore_tls` | 跳过 TLS 证书验证（仅 HTTP） | false |
| `enabled` | 启用/禁用监控（null = 启用） | true |
| `notifier_ids` | 仅通知指定渠道（空 = 不发送通知） | [] |

### 监控类型

| 类型 | Target 格式 | 示例 |
|---|---|---|
| `http` | 完整 URL | `https://api.example.com/health` |
| `tcp` | `主机:端口` | `db.example.com:5432` |
| `ping` | 主机名或 IP | `10.0.0.1` |

> **注意：** Ping 使用系统 `ping` 命令，无需特殊权限。请确保 `ping` 在系统 `PATH` 中可用。

### 数据文件

| 文件 | 说明 |
|---|---|
| `config.json` | 所有配置（系统、认证、分组、监控项） |
| `history.json` | 延迟历史和可用率数据 |
| `incidents.json` | 故障记录，自动保留 30 天 |

## 开发

```bash
# 开发模式运行
make dev

# 代码格式化
make fmt

# 静态分析
make vet

# 运行测试
make test

# 完整构建（Tailwind + 格式化 + 静态分析 + 编译）
make build

# 仅重新编译 Tailwind CSS
make tailwind

# 交叉编译所有平台
make cross
```

## API

### 健康检查

```
GET /healthz
```

返回（无需认证）：

```json
{
  "status": "ok",
  "version": "0.1.0",
  "uptime_seconds": 86400,
  "monitor_count": 5
}
```

## 架构

```
调度器 → 每个监控项一个 goroutine → 探测器 (HTTP/TCP/ICMP)
      → 分析器 (防抖控制) → 通知路由 → Telegram / Webhook
                          → 历史管理器 → history.json + incidents.json (原子写入)
```

- **前端：** 原生 JavaScript + Tailwind CSS（本地编译嵌入），通过 `go:embed` 打包
- **后端：** Go + chi 路由，`html/template` 渲染
- **存储：** 原子 JSON 文件写入（写入 → 同步 → 重命名）

## 许可证

GPL-3.0
