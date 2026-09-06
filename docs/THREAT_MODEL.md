# 威胁模型（v1）

信任边界为浏览器—WebSSH HTTPS/WSS、WebSSH—SSH 目标和进程—SQLite/Master Key。终端输出、主机名、标签和备注均视为不可信输入。

主要控制：bcrypt 密码哈希；随机服务端会话与 HttpOnly/SameSite Cookie；生产 Secure Cookie；Origin 与 CSRF 双重校验；严格 CSP 与禁止 iframe；WebSocket Upgrade 前鉴权；Host Key TOFU 后固定；AES-256-GCM 凭据封装且 Master Key 不进 SQLite；metadata 目标 denylist 和管理端口回环阻断；禁用 forwarding（未请求任何 forwarding）；连接/握手超时与 keepalive；消息限额、会话限额和有界 backpressure；审计不记录 secret 与终端内容。

剩余风险：可信管理员可配置通向内网的 SSH 目标；内存中的解密凭据可能受主机级入侵影响；HTTP 开发模式不能防窃听；TOFU 首次确认依赖用户通过独立渠道核对；v1 的 Web 会话状态在进程重启后失效，终端断线后应依赖远端 tmux 保持工作。
