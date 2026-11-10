# GoChat

![CI](https://github.com/traveller0x/gochat/actions/workflows/ci.yml/badge.svg)

用 Go 实现的即时通讯服务：WebSocket 长连接、会话级消息序号、重发幂等、断线补洞、
Redis 在线状态与多实例消息路由；前端为 Vue 3 + TypeScript。

## 功能

- **账号**：注册 / 登录（JWT）、用户搜索
- **单聊**：幂等创建会话、会话列表（未读数 + 最后一条消息 + 对方信息）
- **消息**：实时收发、会话内连续 `seq`、`client_msg_id` 幂等、失败重试
- **历史**：`before_seq` 游标分页；`after_seq` 断线补洞
- **已读**：`last_read_seq` 上报、多端已读同步、未读数归零
- **长连接**：单写者模型、有界发送队列、慢客户端摘除、心跳与超时
- **多实例**：Redis 在线状态（TTL 自愈）+ Pub/Sub 跨实例投递
- **防护**：按用户令牌桶限流、单连接帧率上限、连接数上限、Origin 白名单
- **可观测**：结构化日志（`slog` + `request_id`）、Prometheus 指标

## 架构

```
        Vue 3 SPA (nginx)                  Web / 移动端
              │  HTTP + WebSocket
              ▼
        ┌─────────────┐
        │   gochat    │   × N 个实例（无状态）
        │  Gin + WS   │
        └──┬───────┬──┘
           │       │
   ┌───────▼──┐  ┌─▼───────────────┐
   │  MySQL   │  │      Redis      │
   │ 用户/会话 │  │ 在线状态         │
   │ /消息     │  │ Pub/Sub 路由     │
   └──────────┘  └─────────────────┘
```

消息的可靠性**不依赖 Redis**：`messages` 表是唯一事实来源，`seq` 提供顺序，
客户端按 `last_read_seq` 与本地最大 `seq` 补拉；Redis 只负责"在线实时投递"，
故障时降级为"重连后补齐"，不影响一致性。

## 技术栈

| 层 | 选型 |
|---|---|
| 语言 | Go 1.26 |
| HTTP | Gin |
| 长连接 | gorilla/websocket |
| 数据库 | MySQL 8.4 + GORM；版本化迁移用 golang-migrate |
| 缓存 | Redis 7（go-redis）：在线状态、Pub/Sub、限流 |
| 认证 | JWT HS256 + bcrypt |
| 可观测 | `log/slog`、Prometheus |
| 前端 | Vue 3 + TypeScript + Vite + Pinia + Naive UI |
| 部署 | Docker 多阶段构建 + docker-compose + nginx |
| CI | GitHub Actions（gofmt、`go test -race`、前端类型检查与构建） |

## 快速开始（Docker）

```bash
cp .env.example .env     # 填入数据库/Redis 密码与 JWT 密钥
docker compose up -d --build
```

- Web：<http://localhost:8088>
- API：<http://localhost:8080>
- 指标：<http://localhost:8080/metrics>

`docker compose up` 会依次启动 MySQL、Redis、数据库迁移（一次性任务）、后端、前端。

## 本地开发

```bash
# 1. 依赖
docker compose up -d mysql redis
make migrate-up

# 2. 后端 http://127.0.0.1:8080
make run

# 3. 前端 http://localhost:5173
make web-install
make web-dev
```

Vite 开发服务器会把 `/api` 代理到 8080（含 WebSocket），无需处理 CORS。
测试账号可直接用登录页的「注册」创建。

## 配置

配置文件为 `configs/config.yaml`，敏感值与部署差异通过环境变量注入：

| 变量 | 说明 |
|---|---|
| `MYSQL_ROOT_PASSWORD` | docker compose 初始化 MySQL 使用 |
| `GOCHAT_MYSQL_PASSWORD` | 应用连接 MySQL 的密码 |
| `GOCHAT_MYSQL_HOST` / `GOCHAT_MYSQL_PORT` | 容器部署时覆盖为 `mysql` / `3306` |
| `GOCHAT_JWT_SECRET` | JWT 密钥，至少 32 字节 |
| `REDIS_PASSWORD` | docker compose 初始化 Redis 使用 |
| `GOCHAT_REDIS_PASSWORD` | 应用连接 Redis 的密码 |
| `GOCHAT_REDIS_HOST` / `GOCHAT_REDIS_PORT` | 容器部署时覆盖为 `redis` / `6379` |
| `GOCHAT_SERVER_PORT` | 覆盖监听端口（多实例演示用） |
| `GOCHAT_ENV` | `development` / `production`（生产输出 JSON 日志） |
| `GOCHAT_RATE_LIMIT_PER_SECOND` | 每用户每秒消息数，默认 20 |
| `GOCHAT_RATE_LIMIT_BURST` | 每用户突发量，默认 40 |
| `GOCHAT_MAX_CONNECTIONS` | 单实例连接上限，默认 10000 |
| `GOCHAT_ALLOWED_ORIGINS` | WebSocket Origin 白名单，逗号分隔 |

## 接口

### REST

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/v1/auth/register` | 注册 |
| POST | `/api/v1/auth/login` | 登录，返回 JWT |
| GET | `/api/v1/users/me` | 当前用户 |
| GET | `/api/v1/users/search?keyword=` | 搜索用户 |
| POST | `/api/v1/conversations` | 创建 / 获取单聊（幂等） |
| GET | `/api/v1/conversations` | 会话列表（未读数、最后一条、对方信息） |
| GET | `/api/v1/conversations/:id/messages?before_seq=&after_seq=&limit=` | 历史 / 补洞 |
| POST | `/api/v1/conversations/:id/read` | 上报已读 |
| GET | `/metrics` | Prometheus 指标 |

统一响应结构：

```json
{ "code": 0, "message": "success", "data": {} }
```

`code` 是业务码（`0` 成功；`40xx` 客户端错误；`50xx` 服务端错误），与 HTTP 状态码配合使用。
例如三种 401 场景：`4011` 缺少 token、`4012` 请求头格式错误、`4013` token 无效或过期。

### WebSocket

连接：`GET /api/v1/ws?token=<JWT>`。浏览器无法为 WebSocket 设置请求头，所以 token 走 query。

统一帧格式：

```json
{ "type": "message.send", "data": {} }
```

| 方向 | type | 说明 |
|---|---|---|
| C→S | `ping` | 保活探测 |
| S→C | `pong` | 保活回应 |
| C→S | `message.send` | 发送消息（携带 `client_msg_id`） |
| S→C | `message.ack` | 落库确认（返回 `id` / `seq` / `duplicated`） |
| S→C | `message.new` | 新消息推送（发给会话全部成员，含发送者其他设备） |
| S→C | `message.read` | 已读位置同步（发给本人其他设备） |
| S→C | `error` | 错误帧（`4001` 参数、`4031` 非成员、`4290` 限流等） |

## 关键设计

1. **会话级 `seq` + 行锁分配**
   发消息事务内 `SELECT ... FOR UPDATE` 锁住会话行，`seq = last_seq + 1`，
   保证同一会话严格有序。代价是单会话写入串行，水平扩展靠"多会话并行 + 多实例"。

2. **重发幂等**
   `UNIQUE(conversation_id, client_msg_id)`；冲突后回查并返回原消息；
   重发不重复推送，客户端用同一个 `seq` 对齐。

3. **先落库、后推送**
   推送失败不影响一致性；客户端重连后用 `after_seq` 补洞。

4. **离线消息不是队列**
   `messages` + `last_read_seq` + 客户端本地游标做区间查询，
   不维护第二份存储，也不依赖 MQ。

5. **异步扇出**
   落库后把推送放入有界队列，由固定数量 worker 处理；
   ack 只代表"已持久化"，延迟不受接收方数量影响。队列满则丢弃并计数，
   可靠性由补拉保证。

6. **多实例**
   在线状态用 `online:{userID}` 集合 + TTL 自愈（进程被 kill -9 也能自动清理）；
   跨实例用 `PUBLISH chat:route` 并携带 `origin` 防止回环。
   Pub/Sub 不持久化，因此只承担实时投递。

## 性能

环境：MacBook Air（Apple Silicon），MySQL / Redis 运行在 Docker，
单后端实例，压测客户端与后端同机。

| 场景 | 目标速率 | 实际确认 | 错误 | P50 | P95 | P99 |
|---|---|---|---|---|---|---|
| 单连接（无扇出） | 50/s | 50.0/s | 0 | 5ms | 6ms | 8ms |
| 50 用户 × 2/s | 100/s | 98.3/s | 0 | 43ms | 58ms | 66ms |
| 200 用户 × 2/s | 400/s | 393/s | 0 | 106ms | 144ms | 161ms |

解读：

- 单连接 P50 约 5ms，说明单条消息链路（校验 + 事务 + 入队推送）本身很轻；
- 并发上升后，P50 主要来自 **MySQL 连接池排队（默认 25）** 与行锁排队；
- 同一会话内严格有序是设计目标，因此单会话吞吐有上限；
- 复现方式：`go run ./cmd/loadtest -users 200 -rate 2 -duration 30s`。

## 目录结构

```
cmd/server        服务入口
cmd/loadtest      WebSocket 压测客户端（多用户多会话）
internal/
  auth            JWT 签发与校验
  config          配置加载（YAML + 环境变量覆盖）
  database        MySQL / Redis 连接
  handler         HTTP / WebSocket 处理器
  logging         slog 封装与 request_id 注入
  metrics         Prometheus 指标
  middleware      JWT、请求日志、指标采集
  model           数据模型
  presence        在线状态（Redis）
  ratelimit       按用户令牌桶
  repository      数据访问层
  service         业务逻辑
  ws              Hub、读写泵、跨实例路由、协议
migrations        版本化 SQL 迁移
web               Vue 3 前端
```

## 测试与 CI

```bash
make check              # gofmt + vet + go test -race（集成测试自动跳过）
make test-integration   # 需要 .env 中的 MySQL 密码，跑真实数据库测试
```

覆盖点：并发创建会话、并发分配 `seq`、重发幂等、游标分页、已读只进不退、
Hub 并发安全、连接泄漏（goleak）、前端类型检查。

CI 在每次 push 时执行：后端 `make check`、前端 `vue-tsc + vite build`。

## 已知限制

- 暂不支持群聊、图片 / 文件消息；
- `seq` 由单库行锁分配，单会话写入是串行的；
- 在线状态是实例级（同一实例多条连接合并为一个成员）；
- token 存在于 localStorage 与 WebSocket query 中，生产建议改为短时 ticket。

## Roadmap

- [ ] 群聊（成员管理、群消息扇出）
- [ ] 图片 / 文件消息（对象存储预签名直传）
- [ ] 好友关系
- [ ] Tauri 桌面客户端


