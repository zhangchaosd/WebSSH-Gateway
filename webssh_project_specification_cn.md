# WebSSH Gateway产品需求与技术设计规格书

面向局域网、自托管、浏览器 SSH 终端与主机会话管理技术基线：Go + xterm.js + SQLite

版本：v1.0日期：2026-09-06文档用途：交付专业研发团队进行产品设计、开发、测试与部署

# 1. 文档目标与项目定义

本项目拟开发一个轻量、自托管的 Web SSH Gateway。服务部署在局域网内的一台 Linux 主机或虚拟机上，用户通过桌面浏览器、iPad Safari 或移动浏览器访问 Web 页面，管理预先保存的 SSH 目标，并在浏览器中直接获得完整交互式终端。产品定位介于 WeTTY 与轻量堡垒机之间：比单一 WebTTY 提供更完整的主机、凭据和会话管理，但不追求大型企业堡垒机的审批、工单和复杂 IAM。

第一优先级是“终端行为正确”。必须支持 bash/zsh、tmux、vim/neovim、top/htop、less、mc 等依赖 PTY、ANSI/VT 控制序列、窗口尺寸、全屏 alternate screen、功能键、组合键和鼠标事件的 TUI 程序。不得采用仅能执行命令并返回文本的伪终端方案。

# 2. 产品目标、非目标与约束

| 类别 | 定义 |
| --- | --- |
| 核心目标 | 浏览器即 SSH 客户端；保存主机；安全管理凭据；多终端 Tab；完整 PTY；iPad 可用；低资源占用；单机易部署。 |
| 性能目标 | 在 2 vCPU / 1 GiB RAM / 8 GiB 磁盘 Fedora VM 上可稳定服务小团队；SSH 数据链路低延迟。 |
| 部署目标 | 优先提供单 Go 二进制 + 静态前端内嵌；同时提供 systemd 与容器部署。 |
| 安全目标 | 默认拒绝未知 Host Key；凭据静态加密；WSS/HTTPS；会话级鉴权；CSRF/Origin 防护；审计关键管理操作。 |
| 非目标 | v1 不实现 RDP/VNC、SFTP 文件管理、企业审批流、LDAP/AD、Kubernetes Web Console、跨地域 HA 集群。 |
| 网络边界 | 默认仅面向可信局域网或经 VPN 接入；若暴露公网，必须置于 TLS、强认证、反向代理/WAF/VPN 等额外控制之后。 |

# 3. 目标用户与典型场景

- 家庭实验室 / Homelab：管理 PVE、OpenWrt、NAS、Linux VM、交换机等。
- 小型研发/运维团队：通过统一 Web 页面访问实验室或内网 SSH 设备。
- iPad 用户：无需安装专用 SSH App，Safari 打开网页即可使用 tmux/vim。
- 临时终端：在不可信或受限客户端上，不落地 SSH 私钥，只通过服务端代理访问。
典型用户旅程：登录 → 搜索/选择主机 → 打开终端 → 服务端建立 SSH → 请求 PTY → 进入 Shell → 可启动/恢复 tmux → 多 Tab 工作 → 断开或关闭。

# 4. 总体架构

```text
Browser (xterm.js)  │ HTTPS / WSS  │  ├─ REST: 主机/分组/凭据/设置  │  └─ WebSocket: terminal binary stream + control frames  ▼Go WebSSH Server  ├─ Auth / Session / RBAC  ├─ Host & Credential Service  ├─ Terminal Gateway  │    ├─ SSH Client  │    ├─ PTY Request  │    ├─ stdin/stdout/stderr bridge  │    └─ WindowChange / keepalive  ├─ SQLite  ├─ Secret Encryption  └─ Embedded Web Assets  │  └──────── SSH/TCP ────────> LAN targets
```

前端使用 xterm.js。其官方项目明确支持 bash、vim、tmux、curses 应用及鼠标事件，因此适合作为浏览器终端渲染层。后端不在本机启动 ssh CLI + PTY，而是优先直接使用 Go SSH 协议库建立远端 SSH Session，从而减少本机进程、PTY 与 shell 注入面。

# 5. 技术选型

| 层 | 推荐技术 | 说明 |
| --- | --- | --- |
| 后端 | Go | 单二进制、低内存、并发模型适合大量 I/O 长连接。 |
| SSH | golang.org/x/crypto/ssh | 支持 RequestPty、Shell、WindowChange 等 SSH Session 能力。 |
| HTTP | Go net/http 或成熟轻量 Router | 避免过重框架；WebSocket 库需支持 ping/pong、限额、超时和 binary frame。 |
| 数据库 | SQLite (WAL) | 单节点足够；备份简单；v1 不引入独立 PostgreSQL。 |
| 前端 | TypeScript + xterm.js | 终端渲染与键盘输入核心。 |
| UI | 轻量 SPA | React/Vue/Svelte 可任选，团队统一即可；不得让 UI 框架侵入终端数据通道。 |
| 打包 | go:embed | 生产构建将前端 dist 内嵌到 Go 二进制。 |
| TLS | 内置 TLS 或 Caddy/Nginx | 推荐反代终止 TLS；应用必须正确识别 Secure Cookie / Proxy headers。 |

# 6. tmux / TUI 兼容性：强制性设计要求

tmux 支持不是“能够输入 tmux 命令”即可验收，而是完整 PTY 与终端语义必须正确。xterm.js 官方说明其可运行 tmux 等终端应用；Go SSH 库提供 RequestPty 与 WindowChange。实现必须把浏览器终端尺寸变化传递到远端 PTY，否则 tmux/vim 会出现窗格尺寸错误、重绘异常或状态栏错位。UI审美要现代、简洁、美观。

| 要求 | 实现要求 | 验收 |
| --- | --- | --- |
| PTY | SSH 建连后 RequestPty，TERM 默认 xterm-256color，可配置；随后 Shell。 | 执行 tty 返回有效 /dev/pts/*；stty size 与浏览器一致。 |
| Resize | xterm FitAddon/ResizeObserver 获取 cols/rows；debounce 后发送 control frame；后端调用 WindowChange(rows, cols)。 | 拖动窗口、旋转 iPad 后 tmux pane/vim 自动重排，无需手动 redraw。 |
| 原始字节流 | 终端 stdout 不做 UTF-8 文本重写、换行转换或 HTML 处理；WebSocket 数据帧按字节传输。 | ANSI、OSC、UTF-8/CJK、emoji 不被代理层破坏。 |
| Ctrl/Alt/Esc | 前端不得截获终端常用组合键；仅对浏览器保留键做有意识处理。 | Ctrl-B（tmux 默认前缀）、Ctrl-A、Ctrl-C、Ctrl-D、Ctrl-Z、Esc、Alt 组合可用。 |
| 功能键 | 正确发送 F1-F12、Home/End、Insert/Delete、PgUp/PgDn、方向键序列。 | vim/tmux/less 中按键行为符合本地终端。 |
| Alternate Screen | 不得缓存/过滤切屏控制序列。 | vim/less 退出后主屏内容恢复正确。 |
| 鼠标 | xterm.js mouse reporting 透传；浏览器选择文本与终端鼠标模式需定义 UX。 | tmux set -g mouse on 后窗格选择/滚动可用。 |
| 粘贴 | 支持 bracketed paste；不自行添加 CR/LF。 | vim/shell 大段粘贴不产生异常缩进或重复换行。 |
| 颜色 | 至少 256 色；TrueColor 尽力支持。 | tmux/vim 主题无明显颜色退化。 |

建议兼容测试命令：

```text
echo $TERMttystty -aprintf '\e[31mRED\e[0m\n'tmux new -s webssh-testtmux split-window -htmux split-window -vtmux set -g mouse onvim /tmp/test.txthtop
```

# 7. WebSocket 终端协议

不得简单把所有 WebSocket 消息当字符串。建议定义版本化的双通道语义：Binary Frame 专用于 SSH stdin/stdout 原始字节；Text/JSON Frame 仅用于控制消息。协议必须允许未来加入重连、心跳、只读观察等能力。

```text
Client -> Server control:{"v":1,"type":"resize","cols":120,"rows":40}{"v":1,"type":"ping","ts":...}{"v":1,"type":"signal","name":"INT"}   // 可选，不作为 Ctrl-C 主路径Client -> Server binary:<raw terminal input bytes>Server -> Client binary:<raw SSH PTY output bytes>Server -> Client control:{"v":1,"type":"ready","session_id":"..."}{"v":1,"type":"exit","code":0}{"v":1,"type":"error","code":"SSH_AUTH_FAILED","message":"..."}
```

- WebSocket Upgrade 前完成 HTTP Session 鉴权与 Host 授权，不允许连接后再信任客户端声明的 user_id。
- 校验 Origin；生产环境仅允许配置的 Web Origin。
- 设置单帧与累计消息大小限制，避免内存 DoS。
- 实现 ping/pong 与 idle timeout；终端活跃时刷新会话。
- 同一终端只允许一个 writer goroutine 写 WebSocket，避免并发写导致帧损坏。
- SSH stdout 读取必须持续 drain，避免远端因缓冲区阻塞。
# 8. 功能需求

FR-001  用户登录

系统必须提供本地账户认证。v1 至少支持用户名+密码；密码使用 Argon2id 或 bcrypt 进行单向哈希。

验收标准：未登录访问 API/WSS 被拒绝；登录后使用 HttpOnly、Secure、SameSite Cookie；退出立即失效。

FR-002  主机管理

支持新增、编辑、删除、复制 SSH 主机，字段至少含名称、地址、端口、用户名、凭据、分组、标签、备注。

验收标准：可管理 IPv4/IPv6/DNS 主机；默认端口 22；列表可搜索和排序。

FR-003  分组与收藏

支持树状或单层分组、标签、收藏。

验收标准：100 台主机情况下 2 秒内定位常用目标。

FR-004  SSH 密码认证

凭据可保存 SSH 密码，数据库不得明文存储。

验收标准：数据库文件直接查看不能恢复明文；正确密钥可解密使用。

FR-005  SSH Key 认证

支持 PEM/OpenSSH 私钥；支持有 passphrase 私钥。

验收标准：Ed25519/RSA 至少通过；私钥不返回浏览器。

FR-006  Host Key 校验

首次连接显示/登记指纹；后续变更必须阻断并告警。

验收标准：禁止默认 InsecureIgnoreHostKey。

FR-007  浏览器终端

点击主机后打开 xterm.js 终端并建立 SSH Shell。

验收标准：Shell 可连续交互 8 小时；无随机断流、乱码或键盘失效。

FR-008  多 Tab

同一浏览器可同时打开多个 SSH Terminal Tab。

验收标准：Tab 间输入输出隔离；关闭一个不影响其他连接。

FR-009  终端 Resize

浏览器尺寸改变时同步远端 PTY。

验收标准：stty size、tmux、vim 实时匹配。

FR-010  tmux 兼容

完整支持 tmux 创建、attach、detach、split、resize、mouse、copy-mode 常见操作。

验收标准：通过第 18 节 tmux 验收矩阵。

FR-011  iPad 终端工具条

移动端提供 Esc、Tab、Ctrl、Alt、方向键、PgUp/PgDn 等辅助键。

验收标准：Safari 软键盘下可完成 tmux Ctrl-B、vim Esc 等操作。

FR-012  剪贴板

支持复制与粘贴；需符合浏览器权限模型。

验收标准：桌面与 iPad 均有明确复制/粘贴入口；失败时给出可理解提示。

FR-013  终端搜索

支持当前 scrollback 搜索。

验收标准：Ctrl/Cmd+F 或 UI 可查找终端历史。

FR-014  连接状态

显示 Connecting/Connected/Reconnecting/Disconnected/Failed。

验收标准：网络异常与认证失败可区分。

FR-015  审计

记录登录、主机/凭据变更、连接开始/结束、失败原因。

验收标准：审计不记录密码、私钥、终端输入内容。

FR-016  设置

管理员可设置 idle timeout、最大并发、允许 Origin、SSH connect timeout、keepalive 等。

验收标准：配置修改有校验且可持久化。

FR-017  会话编码

支持对保存的连接设置默认编码，GB18030或UTF8，默认UTF8。也可以对已经打开的连接更该编码格式。

验收标准：SSH中打印GB18030和UTF8字符时如果编码选择正确则可以正常显示。

# 9. 会话生命周期与断线策略

v1 建议将“浏览器 WebSocket”与“SSH Session”一一绑定。浏览器断线后进入短暂 grace period（建议默认 15 秒，可配置）；若同一 authenticated browser session 携带一次性 reconnect token 回来，可重新绑定输出；超时则关闭 SSH Session。注意：这不是 tmux 的替代品。长期持久会话应由远端 tmux 提供。

- 推荐 UI 提供“连接后自动执行命令”可选项，但默认关闭。可用于 `tmux new-session -A -s main`。
- 自动命令必须是主机级管理员配置，且明确提示其安全影响；不能拼接用户未校验输入。
- 若 SSH 断开，前端应保留 scrollback 并显示退出原因；禁止无提示清屏。
# 10. 数据模型

| 表 | 关键字段 |
| --- | --- |
| users | id, username, password_hash, role, enabled, created_at, last_login_at |
| host_groups | id, name, parent_id, sort_order |
| credentials | id, name, type, username(optional), encrypted_secret, key_fingerprint, created_at, updated_at |
| hosts | id, name, hostname, port, username, credential_id, group_id, favorite, notes, host_key_type, host_key_fingerprint, options_json |
| sessions | id, user_id, host_id, started_at, ended_at, result, client_ip, user_agent |
| audit_events | id, user_id, action, object_type, object_id, metadata_json, created_at |
| settings | key, value_json, updated_at |

数据库约束：启用 foreign_keys；推荐 WAL；迁移必须版本化并支持向前升级。删除凭据时若仍被主机引用，应阻止或要求显式迁移。

# 11. 凭据与密钥保护

- 用户登录密码只保存强哈希，不可解密。
- SSH 密码、私钥及私钥 passphrase 使用应用 Master Key 做 AEAD 加密（推荐 AES-256-GCM 或 ChaCha20-Poly1305）。
- Master Key 不存 SQLite；通过 root-only 文件、systemd credential、容器 secret 或环境注入。
- 密文记录包含 nonce、ciphertext、key version；支持未来 key rotation。
- 日志、panic、API 错误、审计 metadata 严禁输出 secret。
- 私钥解密后只存在进程内存，生命周期限定于建立 SSH 认证所需范围。
- 备份文档必须明确：SQLite 备份与 Master Key 缺一不可恢复保存的 SSH 凭据。
# 12. SSH 安全策略

- Host Key 默认 TOFU：首次连接由有权限用户确认指纹；确认后 pin。
- Host Key 发生变化时硬失败，不提供“自动接受新 key”默认选项。
- 限制可配置的目标端口范围可作为安全增强；服务端应防 SSRF，至少支持 denylist 阻止访问自身管理端口、metadata IP 等。
- 默认禁止 SSH agent forwarding、X11 forwarding、remote/local TCP forwarding；未来若实现必须独立授权。
- SSH keepalive 建议每 30 秒一次，连续若干次失败后断开；具体实现需避免误判高延迟网络。
- 连接建立设置 DNS/TCP/SSH handshake 超时。
# 13. Web 安全要求

| 风险 | 要求 |
| --- | --- |
| WebSocket 劫持 | 校验 Origin；Cookie SameSite；WSS；Upgrade 前鉴权。 |
| CSRF | 所有状态变更 REST API 使用 CSRF token 或严格 SameSite + Origin/Referer 策略。 |
| XSS | 终端数据视为不可信；禁止将 OSC title、link 等直接 innerHTML。 |
| 点击劫持 | CSP frame-ancestors 'none' 或等价 X-Frame-Options。 |
| 暴力登录 | IP/账户维度限速与指数退避；可配置锁定策略。 |
| Session fixation | 登录后 rotate session id；退出服务端销毁。 |
| CSP | 默认严格 CSP；静态资源 self；不依赖公共 CDN。 |
| 供应链 | 前端依赖 lockfile；CI 做漏洞扫描与 license 检查。 |

xterm.js 官方安全指南特别提示：终端数据必须视为不可信，WebSocket 需要自行增加 WSS、认证与授权保护；不得直接把其 demo/attach 示例当成生产安全实现。

# 14. 前端 UX 规格

- 桌面布局：左侧主机/分组导航；顶部全局搜索；中间终端工作区；顶部或底部 Terminal Tabs。
- iPad：主机列表可折叠；终端占满视口；考虑 Safari 动态地址栏与软键盘导致的 viewport 高度变化。
- 终端工具条可配置：Esc、Tab、Ctrl、Alt、↑↓←→、Home、End、PgUp、PgDn、Ctrl-B、Paste。
- Ctrl/Alt 可设计为一次性 latch：点击 Ctrl 后，下一个字符组合发送，再自动释放。
- 触控滚动：非 mouse-reporting 时滚动 scrollback；tmux mouse mode 开启后优先向终端发送 mouse event，并提供显式“选择文本”模式。
- Terminal Tab 关闭时若仍连接，要求二次确认；可配置关闭不确认。
- 连接错误要区分 DNS、TCP timeout、Host Key mismatch、SSH auth failed、permission denied、server disconnected。
# 15. REST API 草案

| Method | Path | 用途 |
| --- | --- | --- |
| POST | /api/v1/auth/login | 登录 |
| POST | /api/v1/auth/logout | 退出 |
| GET | /api/v1/me | 当前用户 |
| GET/POST | /api/v1/hosts | 主机列表/新增 |
| GET/PUT/DELETE | /api/v1/hosts/{id} | 主机详情/更新/删除 |
| POST | /api/v1/hosts/{id}/verify-host-key | 首次指纹确认 |
| GET/POST | /api/v1/credentials | 凭据列表/新增（列表绝不返回 secret） |
| PUT/DELETE | /api/v1/credentials/{id} | 更新/删除凭据 |
| GET/POST | /api/v1/groups | 分组 |
| GET | /api/v1/audit | 审计查询 |
| GET/PUT | /api/v1/settings | 系统设置 |
| GET | /api/v1/healthz | 进程健康 |
| GET | /api/v1/readyz | 数据库/关键依赖就绪 |
| WS | /api/v1/terminal/{host_id} | 终端数据通道 |

API 返回统一错误结构：

```text
{  "error": {    "code": "SSH_HOST_KEY_MISMATCH",    "message": "Host key changed",    "request_id": "..."  }}
```

# 16. 后端模块划分

```text
cmd/webssh/internal/  app/            // composition root  auth/           // login/session/password hashing  host/           // host/group domain  credential/     // encryption/decryption  sshgw/          // SSH dial, host key, PTY, bridge  terminal/       // websocket protocol, lifecycle, resize  audit/  store/          // SQLite repositories + migrations  config/  httpapi/  security/web/  src/  dist/           // embedded at release buildmigrations/tests/docs/
```

sshgw 与 terminal 必须可单元测试，不能把 SSH、WebSocket、数据库全部耦合在 HTTP handler 中。建议以接口注入 dialer、clock、store，便于故障测试。

# 17. 性能与资源目标

| 指标 | v1 目标 |
| --- | --- |
| 基线机器 | Fedora, 2 vCPU, 1 GiB RAM, 8 GiB disk |
| 空闲 RSS | 建议 < 120 MiB（不含反代）；目标而非硬 SLA |
| 10 个并发 SSH | 无明显输入延迟；服务 RSS 建议 < 250 MiB |
| WebSocket 输入延迟 | LAN 环境代理增加的 p50 延迟目标 < 20 ms |
| 主机列表 | 1,000 主机查询/搜索可用；典型规模 < 200 |
| 日志 | 默认轮转；不得无限增长占满 8 GiB 磁盘 |
| 数据库 | SQLite WAL；定期 checkpoint；备份时保证一致性 |

- 终端输出必须采用 backpressure 策略，禁止无限 channel/buffer。慢客户端达到阈值后应断开或丢弃会话，而不是 OOM。
- 前端 scrollback 默认建议 5,000-10,000 行，可配置上限；移动设备默认更保守。
- 避免把终端每个字节写审计日志或数据库。
# 18. tmux / 终端专项验收矩阵

| 编号 | 测试 | 通过标准 |
| --- | --- | --- |
| T-01 | tmux new -s test | 正常进入，状态栏完整。 |
| T-02 | Ctrl-B c / n / p | 创建/切换 window 正常。 |
| T-03 | Ctrl-B % 与 Ctrl-B " | 横/纵 split 正常。 |
| T-04 | 浏览器连续 resize | 所有 pane 及时重排，无永久错位。 |
| T-05 | iPad 横竖屏切换 | cols/rows 更新，tmux/vim 重绘正确。 |
| T-06 | tmux set -g mouse on | 点击 pane、滚动/copy-mode 基本可用。 |
| T-07 | tmux detach / attach | detach 返回 shell；重新 attach 状态保留。 |
| T-08 | vim/neovim inside tmux | Insert/Esc/Ctrl/Alt/方向键/颜色正常。 |
| T-09 | htop inside tmux | 全屏刷新、F 键与方向键正常。 |
| T-10 | 中文输入/显示 | UTF-8 CJK 宽字符对齐无明显错位。 |
| T-11 | 长输出 | 连续输出数十 MB 不导致服务 OOM。 |
| T-12 | 断网 5 秒再恢复 | 若在 grace period 内按设计重连；否则明确断开；远端 tmux 可重新 attach。 |
| T-13 | Bracketed paste | shell/vim 粘贴不产生额外换行。 |
| T-14 | Ctrl-C/Ctrl-Z/Ctrl-D | 远端程序收到正确控制字符。 |
| T-15 | less/vim alternate screen | 退出后原 shell 屏幕恢复合理。 |

# 19. 测试策略

- 单元测试：配置、加密、权限、Host Key 比对、WebSocket control frame、resize debounce、数据模型。
- 集成测试：使用容器启动 OpenSSH Server，测试 password/key auth、PTY、shell、resize、exit status。
- E2E：Playwright 驱动 Chromium；Safari/iPad 需真机或 BrowserStack 类环境做补充。
- 终端 Golden Test：发送已知 ANSI/VT 序列，验证不会被后端改写；前端侧以行为测试为主。
- 安全测试：CSRF、跨 Origin WebSocket、session fixation、越权 host_id、secret 泄漏、Host Key mismatch。
- 压力测试：10/25/50 WebSocket+SSH 长连接；慢读客户端；大输出；反复 connect/disconnect。
- 故障注入：DNS 失败、TCP reset、SSH handshake 卡死、SQLite busy、磁盘满、Master Key 缺失。
# 20. 部署规格

## 20.1 systemd 推荐模式

```text
/opt/webssh/webssh/etc/webssh/config.yaml/etc/webssh/master.key     # root:webssh 0640/var/lib/webssh/webssh.db/var/log/webssh/           # 或仅 journald
```

- 创建无登录 shell 的专用 webssh 用户；服务不以 root 运行。
- systemd 设置 Restart=on-failure、NoNewPrivileges=true、PrivateTmp=true 等硬化选项。
- 只赋予访问目标网络所需权限。
## 20.2 容器模式

- 镜像使用 distroless/scratch 或精简基础镜像；以非 root 用户运行。
- 挂载 /data 保存 SQLite；Master Key 使用 secret 注入；前端已内嵌。
- 提供 healthcheck；默认不把数据库或管理文件暴露为 volume web root。
## 20.3 HTTPS

- 推荐 Caddy/Nginx/Traefik 终止 TLS；局域网可使用内部 CA。
- WebSocket 反代必须保留 Upgrade/Connection，合理设置长连接 timeout。
- 应用提供 trusted proxy CIDR 配置，防止伪造 X-Forwarded-For。
# 21. 配置文件草案

```text
server:  listen: "127.0.0.1:8080"  public_url: "https://ssh.example.lan"  trusted_proxies: ["127.0.0.1/32"]database:  path: "/var/lib/webssh/webssh.db"security:  master_key_file: "/etc/webssh/master.key"  allowed_origins: ["https://ssh.example.lan"]  session_idle_timeout: "8h"  login_rate_limit: "10/5m"ssh:  connect_timeout: "10s"  handshake_timeout: "15s"  keepalive_interval: "30s"  reconnect_grace: "15s"  default_term: "xterm-256color"terminal:  scrollback: 10000  resize_debounce: "80ms"  max_ws_message_bytes: 1048576limits:  max_sessions_per_user: 10  max_sessions_total: 50
```

# 22. 可观测性与运维

- 结构化日志：timestamp、level、request_id、user_id、host_id、session_id、event；绝不记录 secret/terminal data。
- metrics 可选：active_sessions、ssh_connect_duration、ssh_failures_total、ws_connections、db_errors、auth_failures。
- /healthz 只反映进程；/readyz 可检查数据库可读写与关键配置。
- 管理界面提供版本号、构建 commit、数据库 schema version。
- SQLite 备份提供 CLI：`webssh backup --output ...`；恢复流程写入运维文档。
- 升级前自动备份数据库；迁移失败必须停止启动，禁止半迁移运行。
# 23. 权限模型

| 角色 | 能力 |
| --- | --- |
| Admin | 用户、主机、凭据、设置、审计、所有 SSH 连接。 |
| Operator | 查看被授权主机并建立 SSH；可管理自己的收藏，不可查看 secret。 |

若首版仅单用户，也应在数据模型与 API 层保留 owner/role/authorization hook，避免未来加入多用户时重构整个终端鉴权链。

# 24. 研发阶段划分

| 阶段 | 交付 |
| --- | --- |
| M0 技术验证 | Go SSH + RequestPty + xterm.js + WebSocket；tmux resize、Ctrl-B、vim、iPad 基础验证。 |
| M1 MVP | 登录、主机 CRUD、密码/Key、Host Key pin、单/多 Tab、SQLite、基础 UI。 |
| M2 可用版 | iPad 工具条、分组搜索、审计、加密 Master Key、断线 grace、错误体系、备份。 |
| M3 加固版 | 安全测试、限流、CSP、systemd hardening、性能/慢客户端、完整 E2E。 |
| M4 Release | 安装包/镜像、升级迁移、管理员手册、用户手册、SBOM、版本发布。 |

# 25. Definition of Done

- 第 18 节全部 P0 终端/tmux 测试通过。
- 所有 secret 静态加密，Host Key 校验不存在 insecure 默认值。
- 跨 Origin WebSocket 与未授权 host_id 测试失败（即被正确阻断）。
- 2C1G Fedora VM 上完成至少 10 并发会话稳定性测试。
- iPad Safari 与至少 Chrome/Edge/Firefox 最新稳定版完成手工验收。
- 提供 systemd 与容器两种部署说明；升级、备份、恢复经过演练。
- 代码有 lint/test/CI；依赖固定；发布物含版本信息与校验和。
- README、管理员手册、API 文档、威胁模型、测试报告齐全。
# 26. 风险与设计决策

| 风险 | 决策/缓解 |
| --- | --- |
| Safari 软键盘与 viewport 不稳定 | 专门适配 visualViewport/ResizeObserver；真机验收。 |
| tmux resize 不正确 | 浏览器 cols/rows -> control frame -> SSH WindowChange，列为 P0 集成测试。 |
| 浏览器快捷键冲突 | 定义快捷键策略；提供移动工具条；尽量不劫持 Ctrl/Alt。 |
| 慢客户端导致内存增长 | 有界缓冲/backpressure/断开策略。 |
| 保存私钥增加风险 | AEAD + 外部 Master Key + 最小权限 + 不回传 secret。 |
| Web 终端成为内网跳板 | 强认证、授权、SSRF/目标限制、审计、默认仅 LAN/VPN。 |
| SQLite 锁 | WAL、短事务、单节点定位；规模超出后再评估 PostgreSQL。 |

# 27. 后续可选功能（不进入 v1 范围）

- TOTP/WebAuthn 二次认证。
- SFTP 文件浏览与上传下载。
- SSH ProxyJump / Bastion 链路。
- 会话共享/只读观察。
- 终端录像与回放（需单独评估隐私、存储和合规）。
- OIDC/LDAP/AD。
- PostgreSQL 与多实例 HA。
- PWA/离线壳，但 SSH 本身仍需网络。
- 自动 `tmux new-session -A -s <profile>` 的可视化 Profile。
# 28. 研发团队必须优先完成的 Spike

在任何完整 UI 开发前，先完成一个不超过数天的终端技术 Spike。其唯一目的不是“页面好看”，而是证明真实终端链路满足 tmux。

1. 浏览器 xterm.js 建立 WSS；Go 建立 SSH。
1. RequestPty("xterm-256color", rows, cols, modes)，随后 Shell。
1. Binary WebSocket 双向桥接原始字节。
1. FitAddon + ResizeObserver -> resize JSON -> Session.WindowChange(rows, cols)。
1. 真机 iPad 测试 Ctrl-B、Esc、方向键、横竖屏。
1. 运行 tmux split + vim + htop + mouse mode。
1. 通过后冻结 terminal protocol v1，再开始主机管理、账户、数据库等产品层开发。
# 29. 参考资料与实现依据

以下资料用于验证关键技术可行性，开发时应以各项目/库当前官方文档为准：

- xterm.js 官方仓库 — https://github.com/xtermjs/xterm.js — 官方说明支持 bash、vim、tmux、curses 应用与鼠标事件。
- xterm.js Addons — https://xtermjs.org/docs/guides/using-addons/ — FitAddon 用于根据容器尺寸调整终端。
- xterm.js Security — https://xtermjs.org/docs/guides/security/ — 强调终端数据不可信、WebSocket 需 WSS 与额外认证授权。
- Go x/crypto/ssh Session — https://pkg.go.dev/golang.org/x/crypto/ssh — 提供 RequestPty 与 WindowChange 等 SSH Session API。
# 附录 A：关键实现伪代码

```text
// 建立 SSH 后（伪代码）session, err := client.NewSession()if err != nil { ... }modes := ssh.TerminalModes{    ssh.ECHO: 1,    ssh.TTY_OP_ISPEED: 14400,    ssh.TTY_OP_OSPEED: 14400,}if err := session.RequestPty("xterm-256color", rows, cols, modes); err != nil { ... }stdin, _ := session.StdinPipe()stdout, _ := session.StdoutPipe()stderr, _ := session.StderrPipe()if err := session.Shell(); err != nil { ... }// WS binary -> stdin.Write(raw)// stdout/stderr -> WS binary// resize control -> session.WindowChange(rows, cols)// session.Wait() -> exit control frame
```

注意：实际实现需要处理 goroutine 生命周期、context cancellation、单 writer、错误合并、stderr 与 PTY 语义、WebSocket close handshake、限流和 backpressure；以上仅用于说明架构，不应直接作为生产代码。

# 附录 B：产品验收最小演示脚本

```text
1. iPad Safari 打开 https://ssh.example.lan 并登录。2. 搜索 “pve01”，点击连接。3. 执行：tmux new -s demo4. Ctrl-B %，再 Ctrl-B "，形成三个 pane。5. 在 pane 1 打开 vim；pane 2 运行 htop；pane 3 tail -f 日志。6. iPad 从横屏切换竖屏，再切回横屏。7. 确认三个 pane 均正确 resize，无错位。8. 开启 tmux mouse，切换 pane、滚动。9. Ctrl-B d detach；关闭 Web Terminal。10. 新建连接，执行 tmux attach -t demo，确认原工作状态仍在。11. 修改测试服务器 Host Key，重新连接必须被系统阻断并显示指纹变化警告。
```
