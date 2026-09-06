# WebSSH Gateway

中文 · [English](README.md)

面向可信局域网与 VPN 的轻量、自托管 Web SSH 网关。后端为 Go 单二进制，内嵌 xterm.js 前端，数据保存在 SQLite。

WebSSH Gateway 提供真正的交互式 PTY，支持 tmux、Vim、htop、less、鼠标事件、alternate screen、功能键、窗口尺寸同步和 bracketed paste。同时具备凭据加密、严格 SSH Host Key 固定、多终端 Tab、移动辅助键、UTF-8/GB18030 动态切换、审计日志及响应式亮/暗主题。

## 主要功能

- 基于 `golang.org/x/crypto/ssh`、`RequestPty` 和 `WindowChange` 的真实 SSH 会话
- 原始二进制 WebSocket 终端数据流与版本化 JSON 控制通道
- SSH 密码、OpenSSH/PEM 私钥及私钥 passphrase
- SSH secret 使用 AES-256-GCM 加密，登录密码使用 bcrypt 哈希
- 首次确认后严格固定 Host Key，指纹变化时硬失败
- 主机、分组、标签、收藏、凭据、设置与审计管理
- 相互隔离的多终端 Tab、scrollback 搜索和剪贴板入口
- iPad/移动工具条：Esc、Tab、Ctrl、Alt、方向键、PgUp/PgDn 和粘贴
- 已打开终端可动态切换 UTF-8 / GB18030
- 可持久化的亮色/深色主题，并同步终端配色
- SQLite WAL、CSRF/Origin 校验、登录限速、CSP 与会话限额
- Docker、systemd、健康检查、备份命令、CI 与自动发版

## 快速开始

可从 [GitHub Releases](https://github.com/zhangchaosd/WebSSH-Gateway/releases) 下载二进制，或在本地构建：

```bash
npm ci
npm run build
go build -trimpath -o webssh ./cmd/webssh

WEBSSH_ADMIN_PASSWORD='请替换为强密码' ./webssh
```

访问 `http://<服务器局域网 IP>:8080`。默认用户名为 `admin`。首次启动必须提供 `WEBSSH_ADMIN_PASSWORD`，密码不会写入日志；登录后请在设置页修改初始密码。

默认监听 `0.0.0.0:8080`，持久化文件写入 `./data`。

## Docker

```bash
export WEBSSH_ADMIN_PASSWORD='请替换为强密码'
docker compose up -d
```

版本标签还会自动发布多架构容器镜像：

```text
ghcr.io/zhangchaosd/webssh-gateway:<版本>
```

## 生产安全配置

默认 HTTP 模式仅供可信局域网内首次验收。生产部署应使用 Caddy、Nginx 或 Traefik 终止 TLS，并设置：

```bash
WEBSSH_PUBLIC_URL=https://ssh.example.lan
WEBSSH_ALLOWED_ORIGINS=https://ssh.example.lan
WEBSSH_COOKIE_SECURE=true
```

应用应放在可信局域网或 VPN 后。恢复加密 SSH 凭据时 SQLite 数据库与 Master Key 缺一不可，请分别备份并严格限制文件权限。

## 使用流程

1. 在“凭据”中添加 SSH 密码或私钥。
2. 在“主机”中添加目标，并选择凭据、分组与默认编码。
3. 首次连接会显示 Host Key 指纹，请通过独立可信渠道核对后固定。
4. 后续 Host Key 发生变化时连接会被阻断，不会自动接受。
5. 终端工具条支持搜索历史、切换 UTF-8/GB18030；移动端提供常用辅助键。

## 配置

应用使用环境变量配置。完整示例见 [deploy/webssh.env.example](deploy/webssh.env.example)。

| 变量 | 默认值 | 用途 |
|---|---|---|
| `WEBSSH_LISTEN` | `0.0.0.0:8080` | HTTP 监听地址 |
| `WEBSSH_DATA_DIR` | `./data` | 持久化目录 |
| `WEBSSH_DATABASE` | `<data>/webssh.db` | SQLite 数据库路径 |
| `WEBSSH_MASTER_KEY_FILE` | `<data>/master.key` | 32 字节 Base64 Master Key 文件 |
| `WEBSSH_ALLOWED_ORIGINS` | 同源 | 逗号分隔的额外 Origin |
| `WEBSSH_COOKIE_SECURE` | `false` | Session Cookie 是否仅允许 HTTPS |
| `WEBSSH_SESSION_IDLE_TIMEOUT` | `8h` | Web 登录会话空闲超时 |
| `WEBSSH_MAX_SESSIONS_PER_USER` | `10` | 单用户终端上限 |
| `WEBSSH_MAX_SESSIONS_TOTAL` | `50` | 全局终端上限 |

## 构建与测试

```bash
npm ci
npm run build
go test -race ./...
go vet ./...
go build -trimpath -o webssh ./cmd/webssh
```

备份命令会先执行 WAL checkpoint，再生成一致的 SQLite 副本：

```bash
./webssh backup --output /safe/path/webssh-backup.db
```

## 终端协议 v1

- 浏览器到服务端 Binary Frame：原始终端输入字节
- 服务端到浏览器 Binary Frame：原始 SSH PTY 输出字节
- Text/JSON Frame：`resize`、`ping/pong`、`encoding`、`ready`、`exit`、`error`
- WebSocket Upgrade 前完成 Cookie 鉴权、Host 授权和 Origin 校验
- 压缩关闭，消息大小、终端会话数和输出缓冲均有上限
- 仅一个 goroutine 写入 WebSocket

更多信息参见 [API 文档](docs/API.md)、[运维手册](docs/ADMIN.md) 与 [威胁模型](docs/THREAT_MODEL.md)。

## 开源协议

[0BSD](LICENSE)——允许自由使用、复制、修改和分发，不要求署名。
