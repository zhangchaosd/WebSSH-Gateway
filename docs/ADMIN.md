# 管理与运维

## systemd

创建无登录 shell 的 `webssh` 用户，安装二进制到 `/opt/webssh/webssh`，数据目录设为 `/var/lib/webssh`。Master Key 推荐由管理员预生成 32 个随机字节并以无填充 Base64 写入 `/etc/webssh/master.key`，权限 `0640 root:webssh`。复制 `deploy/webssh.service` 与环境文件后启动服务。

推荐由 Caddy/Nginx/Traefik 终止 TLS。反向代理须透传 WebSocket Upgrade，并将长连接 timeout 调高。公网部署还应位于 VPN/WAF 后，不建议直接暴露应用端口。

## 备份与恢复

使用 `webssh backup --output ...` 在 WAL checkpoint 后生成一致数据库副本。恢复时停止服务，放回数据库与对应 Master Key，检查所有者/权限后启动。缺少原 Master Key 时保存的 SSH secret 无法解密。

## tmux 验收

连接测试机后依次检查 `tty`、`stty size`、`echo $TERM`。运行 `tmux new -s webssh-test`，测试横纵 split、Ctrl-B、鼠标、vim、htop、less alternate screen、浏览器 resize、iPad 横竖屏与 bracketed paste。代理不改写 UTF-8 原始字节；选择 GB18030 时仅在网关边界转码。

## 故障定位

- `SSH_HOST_KEY_UNKNOWN`：首次连接，核对并确认指纹。
- `SSH_HOST_KEY_MISMATCH`：立即停止，核查目标是否重装或存在中间人风险。
- `SSH_AUTH_FAILED`：检查目标用户名、凭据类型、私钥格式和 passphrase。
- `SLOW_CLIENT`：浏览器消费输出过慢，有界队列主动终止会话以避免 OOM。
- `/readyz` 失败：检查 SQLite 文件权限、磁盘空间和 WAL 状态。
