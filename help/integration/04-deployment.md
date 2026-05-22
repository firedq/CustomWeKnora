# 部署拓扑与运维

> 描述包装层 + WeKnora 的部署形态、网络分层、配置项与升级流程。

---

## 1. 拓扑图

### 1.1 单环境推荐拓扑

```
                       ┌────────────────────┐
                       │  调用方业务系统    │
                       └─────────┬──────────┘
                                 │ HTTPS (公网或专线)
                                 │ Authorization: Bearer <wrapper_jwt>
                                 ▼
                       ┌────────────────────┐
                       │  Wrapper BFF (Go)  │   <─── 公网入口
                       │  cmd/api           │
                       │  :8080             │
                       └─────┬─────┬───────┘
            私网             │     │
       ┌──────────────┐      │     │
       │ Wrapper DB    │◄────┘     │
       │ Postgres      │           │ HTTP /api/v1 + X-API-Key
       │ (db: weknora_ │           │ 私网，无公网入口
       │  wrapper)     │           ▼
       └──────────────┘      ┌──────────────────────────┐
                             │  WeKnora                 │
                             │  cmd/server :8080        │
                             │  Gin /api/v1             │
                             └──┬───────────────────────┘
                                │
        ┌─────────┬─────────────┼──────────────────┬─────────────┐
        ▼         ▼             ▼                  ▼             ▼
   ┌─────────┐ ┌─────────┐ ┌──────────────┐ ┌──────────┐ ┌──────────┐
   │Postgres │ │Redis    │ │Vector DB     │ │DocReader │ │Object    │
   │(db:     │ │         │ │Postgres/ES/  │ │gRPC      │ │Storage   │
   │ weknora)│ │         │ │Qdrant/Milvus │ │(Python)  │ │MinIO/COS │
   └─────────┘ └─────────┘ └──────────────┘ └──────────┘ └──────────┘
```

### 1.2 网络分层

| 层 | 暴露面 | 端口 / 协议 | 鉴权 |
|---|---|---|---|
| 公网 / DMZ | Wrapper BFF | TLS 443 | 包装层 JWT / 包装层 API Key |
| 私网 / SVC 网络 | WeKnora `/api/v1` | HTTP / HTTPS | WeKnora `X-API-Key` |
| 内部 | WeKnora DB / Redis / DocReader / 向量库 | 内部端口 | DB 账户 / 网络白名单 |
| 内部 | Wrapper DB | 内部端口 | DB 账户 |

**关键**：WeKnora 的 `:8080` **不暴露公网**，仅 Wrapper BFF 可达。WeKnora 的 `frontend/` Web UI、`/swagger`、`/health` 等也只在私网。

---

## 2. 部署模式

### 2.1 Docker Compose（推荐起步）

```
deployments/docker-compose.yml
├── service: wrapper-api       (cmd/api 编译产物)
├── service: wrapper-db        (postgres:14)
├── service: weknora-server    (引用 WeKnora 官方镜像或自构镜像)
├── service: weknora-db
├── service: redis
├── service: docreader
└── network: wrapper-net (only wrapper-api ↔ weknora-server)
```

WeKnora 服务**不**绑定到宿主机端口（不 `ports:` 段，仅 `expose:`），保证只在 docker 网络内可达。

### 2.2 Kubernetes

| 资源 | 说明 |
|---|---|
| `Deployment: wrapper-api` | 多副本，HPA |
| `Service: wrapper-api`（ClusterIP） + `Ingress` | 唯一公网入口 |
| `Deployment: weknora-server` | 多副本 |
| `Service: weknora-server`（ClusterIP，**无 Ingress**） | 仅集群内 |
| `NetworkPolicy` | 显式禁止除 `wrapper-api` 之外的 Pod 访问 `weknora-server` |
| `ExternalSecret` / `SealedSecret` | KMS / Vault 注入 |

---

## 3. 配置项（Wrapper BFF）

放在 `config/config.yaml` 或环境变量。

```yaml
server:
  host: 0.0.0.0
  port: 8080
  read_timeout: 30s
  write_timeout: 0          # SSE 不限
  max_upload_size: 200MB

auth:
  jwt:
    issuer: weknora-wrapper
    audience: weknora-clients
    public_key_path: /etc/wrapper/jwt.pub
  api_key:
    enabled: true
    header: X-Wrapper-API-Key

weknora:
  base_url: http://weknora-server:8080   # 私网地址
  request_timeout: 60s                    # 普通请求
  chat_timeout: 0                          # SSE 不超时
  upload_timeout: 600s                     # multipart 上传
  health_path: /health
  register_path: /api/v1/auth/register
  login_path: /api/v1/auth/login
  insecure_skip_verify: false

storage:
  db:
    driver: postgres
    dsn: postgres://wrapper:***@wrapper-db:5432/weknora_wrapper?sslmode=require
    max_open_conns: 25
    max_idle_conns: 5

crypto:
  kms_provider: aws|gcp|vault|local
  kms_key_id: alias/weknora-wrapper
  file_url_secret_path: /etc/wrapper/secrets/file_url_hmac

provisioning:
  owner_email_template: "tenant-{external_key_hash}@wrapper.internal"
  password_strategy: random_64
  retry_max: 5

audit:
  enabled: true
  sink: db                  # 可选: stdout | db | both

rate_limit:
  enabled: true
  store: redis              # 可选: redis | memory
  redis_url: redis://redis:6379/3
  defaults:
    chat_rpm: 60
    upload_rpm: 30
```

---

## 4. 启动与关闭

```
1. wrapper-api 启动
   ├─ 加载配置
   ├─ 连接 wrapper DB（含 migrations 自动执行）
   ├─ 探活 WeKnora /health
   ├─ 启动 HTTP 服务
   └─ 注册信号 handler (SIGTERM)

2. 关闭
   ├─ HTTP server graceful shutdown
   ├─ 拒新请求，等待 SSE 在 max(chat_grace=120s) 内自然结束
   ├─ DB / Redis 连接池关闭
   └─ exit
```

---

## 5. 升级流程

### 5.1 WeKnora 版本升级
1. 拉取新版本 WeKnora → 单独环境部署
2. 对包装层运行**契约测试套件**（见 [05-development-plan.md](./05-development-plan.md) 阶段 7）
3. 若契约测试全过 → 灰度切换
4. 若契约测试失败 → 修改 `internal/weknora/*.go` 适配器（**不**改对外 API）

> 适配器是唯一耦合点：包装层的对外 API 形状由 `02-api-mapping.md` 锁定，WeKnora 升级时仅适配器需要回归。

### 5.2 Wrapper BFF 升级
- 标准蓝绿 / 滚动
- DB 迁移走 migrations 工具，**向前兼容**两个版本：先加列、双写、再切读、再删旧列

---

## 6. 可观测性

| 维度 | 实现 |
|---|---|
| 日志 | 结构化 JSON，包含 `request_id` / `tenant_id` / `wrapper_user_id`；自动脱敏 `X-API-Key` |
| 指标 | Prometheus：QPS、延迟、上游 WeKnora 状态码分布、SSE 活跃数 / 上游连接复用率、轮换失败计数 |
| 链路追踪 | OpenTelemetry，包装层 → WeKnora 单 trace（透传 traceparent） |
| 告警 | WeKnora 健康检查 5xx 占比 > 5%、API Key 解密失败任意一次、provisioning 失败 |

---

## 7. 灾备与备份

| 数据 | 备份策略 |
|---|---|
| `wrapper_tenants` / `wrapper_users` | DB 全量快照 + binlog/WAL，T+1 |
| `weknora_credentials` | 同库备份；KMS 密钥独立备份；备份加密 |
| `audit_events` | 长期归档（冷存储） |
| `wrapper_files` | 仅元数据，原始文件由 WeKnora 对象存储负责 |

**灾难恢复关键点**：恢复 `weknora_credentials` 时若 KMS DEK 丢失 → 不可解密 → **API Key 全部失效**。需要**通过 WeKnora `ResetAPIKey`重新刷出**。所以包装层必须保留 `weknora_owner_email` / 合成 owner 凭证，作为**最终救援通道**。

---

## 8. 调用方对接清单（移交给调用方）

- 包装层 Base URL（如 `https://api.your-product.com/v1`）
- 包装层 OpenAPI / Swagger 文档
- 包装层鉴权方式与 token 获取流程
- 包装层错误码表与重试规范
- 限流策略与配额申请途径
- 文件签名 URL 有效期、缓存策略
- SSE 协议说明（事件类型、心跳、断线重连）

调用方**不需要**了解：WeKnora 的存在 / WeKnora API 形状 / WeKnora 任何凭证。

---

## 9. 待澄清

- DB 共用还是独立实例（一般共实例不同库即可）
- KMS 选型（AWS KMS / Vault / 自建）
- 是否需要多区域 / 多可用区部署
- WeKnora 是否已经在内部部署（决定包装层 base_url 是 sidecar 还是远程地址）
