# API 概览

前缀为 `/api/v1`。除健康检查和登录外均要求 `webssh_session` Cookie；所有状态变更还要求登录响应中的 `csrf_token` 通过 `X-CSRF-Token` 请求头回传。错误统一为 `{ "error": { "code", "message", "request_id", "data" } }`。

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/auth/login` | 用户名密码登录 |
| POST | `/auth/logout` | 注销并销毁服务端会话 |
| GET | `/me` | 当前用户、CSRF token 与版本 |
| PUT | `/me/password` | 校验当前密码后更新登录密码 |
| GET/POST | `/hosts` | 查询/创建主机；`q` 支持名称、地址、标签搜索 |
| GET/PUT/DELETE | `/hosts/{id}` | 主机详情/修改/删除 |
| POST | `/hosts/{id}/copy` | 复制主机，不复制固定 Host Key |
| POST | `/hosts/{id}/verify-host-key` | 二次扫描并固定用户确认的指纹 |
| GET/POST | `/credentials` | 查询元数据/创建加密凭据 |
| PUT/DELETE | `/credentials/{id}` | 修改/删除；被主机引用时拒绝删除 |
| GET/POST | `/groups` | 查询/创建分组 |
| DELETE | `/groups/{id}` | 删除分组，主机自动变为未分组 |
| GET | `/audit` | 管理审计 |
| GET/PUT | `/settings` | 持久设置 |
| GET | `/healthz`, `/readyz` | 存活/SQLite 就绪 |
| WS | `/terminal/{host_id}` | 协议 v1 终端通道 |

凭据列表和所有主机响应均不会包含密码、私钥、passphrase 或密文。终端错误会区分 Host Key 未知/不匹配、认证失败、DNS、超时和一般连接失败。
