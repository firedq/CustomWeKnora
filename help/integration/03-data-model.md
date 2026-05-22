# 包装层数据模型设计

> 仅描述**包装层自有数据库** schema。WeKnora 的数据库**不动**。
>
> 默认数据库：PostgreSQL 14+（与 WeKnora 默认栈一致；SQLite 仅供单机轻量场景）。

---

## 1. 设计原则

1. **身份与凭证分表**：身份信息（`wrapper_tenants` / `wrapper_users`）与敏感凭证（`weknora_credentials`）分库分权
2. **API Key 密文落库**：使用 KMS / Vault 进行封装加密（envelope encryption），明文绝不持久化
3. **幂等开通**：通过 `provisioning_jobs.idempotency_key` 防止重复创建租户
4. **审计可追溯**：每次"创建租户 / 轮换 Key / 删除"产生 `audit_events`
5. **软删 + 唯一索引谨慎使用**：邮箱 / external_key 等唯一约束需 `WHERE deleted_at IS NULL` 的部分唯一索引

---

## 2. 表 schema

### 2.1 `wrapper_tenants` — 外部租户实体

```sql
CREATE TABLE wrapper_tenants (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_key            TEXT NOT NULL,        -- 调用方业务系统主键（不可变）
    display_name            TEXT NOT NULL,
    description             TEXT,
    status                  TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'active', 'frozen', 'failed', 'deleted')),

    -- WeKnora 侧映射（永不下放给调用方）
    weknora_tenant_id       BIGINT NOT NULL,
    weknora_owner_user_id   TEXT,                  -- WeKnora 内合成 owner 的 user id
    weknora_owner_email     TEXT,                  -- 合成 owner 邮箱（用于必要的运维路径）

    provisioning_state      TEXT NOT NULL DEFAULT 'init'
        CHECK (provisioning_state IN ('init', 'registering', 'logging_in', 'storing_key', 'done', 'error')),
    last_error              TEXT,

    metadata                JSONB NOT NULL DEFAULT '{}'::jsonb,

    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at              TIMESTAMPTZ
);

CREATE UNIQUE INDEX uq_wrapper_tenants_external_key
    ON wrapper_tenants(external_key) WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX uq_wrapper_tenants_weknora_tenant_id
    ON wrapper_tenants(weknora_tenant_id) WHERE deleted_at IS NULL;

CREATE INDEX idx_wrapper_tenants_status ON wrapper_tenants(status) WHERE deleted_at IS NULL;
```

| 列 | 说明 |
|---|---|
| `external_key` | 调用方系统中租户的稳定主键，由调用方在 `POST /v1/tenants` 时携带，保证幂等 |
| `weknora_tenant_id` | WeKnora `tenants.id`，永不返回给调用方 |
| `weknora_owner_email` | 包装层为每个租户合成的 owner 邮箱（如 `tenant-{external_key_hash}@wrapper.internal`），仅运维使用 |
| `provisioning_state` | 状态机；用于失败重试 |

---

### 2.2 `wrapper_users` — 包装层独立用户

```sql
CREATE TABLE wrapper_users (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES wrapper_tenants(id) ON DELETE RESTRICT,

    external_user_key   TEXT NOT NULL,        -- 调用方业务系统的 user_id
    email               TEXT,
    display_name        TEXT,

    role                TEXT NOT NULL DEFAULT 'viewer'
        CHECK (role IN ('owner', 'admin', 'editor', 'viewer')),
    status              TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'disabled')),

    -- 鉴权凭证（包装层颁发，非 WeKnora）
    password_hash       TEXT,                  -- 可选：仅当包装层用 username+password
    last_login_at       TIMESTAMPTZ,

    metadata            JSONB NOT NULL DEFAULT '{}'::jsonb,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at          TIMESTAMPTZ
);

CREATE UNIQUE INDEX uq_wrapper_users_tenant_user
    ON wrapper_users(tenant_id, external_user_key) WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX uq_wrapper_users_email
    ON wrapper_users(email) WHERE deleted_at IS NULL AND email IS NOT NULL;

CREATE INDEX idx_wrapper_users_tenant_role
    ON wrapper_users(tenant_id, role) WHERE deleted_at IS NULL;
```

> 包装层用户**不**对应 WeKnora 用户。所有用户在 WeKnora 侧共用同一个合成 owner 的 APIKey 调用上游。WeKnora 内部由租户隔离。

---

### 2.3 `weknora_credentials` — WeKnora API Key 密文（敏感）

```sql
CREATE TABLE weknora_credentials (
    tenant_id               UUID PRIMARY KEY REFERENCES wrapper_tenants(id) ON DELETE CASCADE,
    weknora_tenant_id       BIGINT NOT NULL,

    api_key_ciphertext      BYTEA NOT NULL,        -- KMS 封装后的密文
    api_key_dek_id          TEXT NOT NULL,         -- 数据加密密钥 ID（轮换用）
    api_key_last4           TEXT NOT NULL,         -- 用于审计 / 标识；不可解出明文
    key_version             INT NOT NULL DEFAULT 1,

    rotated_at              TIMESTAMPTZ,
    expires_at              TIMESTAMPTZ,           -- 可选：与上游 APIKey 过期对齐

    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX uq_weknora_credentials_weknora_tenant
    ON weknora_credentials(weknora_tenant_id);
```

**密钥管理要求**
- `api_key_ciphertext` 使用 KMS / Vault / AWS KMS / GCP KMS 等做 envelope encryption
- 明文 APIKey 仅在以下场景出现：
  - 内存中：转发上游前临时解密
  - 网络中：从包装层到 WeKnora（私网 + TLS）
- DB 字段 `api_key_last4` 仅供日志显示，**永不**显示完整明文
- DEK 轮换：`api_key_dek_id` + `key_version` 支持 lazy re-wrap

---

### 2.4 `provisioning_jobs` — 租户开通幂等任务

```sql
CREATE TABLE provisioning_jobs (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID REFERENCES wrapper_tenants(id) ON DELETE CASCADE,
    idempotency_key     TEXT NOT NULL,        -- 调用方提供，或由 external_key 派生
    operation           TEXT NOT NULL
        CHECK (operation IN ('create_tenant', 'rotate_api_key', 'delete_tenant')),
    status              TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'in_progress', 'succeeded', 'failed')),
    attempts            INT NOT NULL DEFAULT 0,
    payload             JSONB NOT NULL DEFAULT '{}'::jsonb,
    result              JSONB,
    last_error          TEXT,
    started_at          TIMESTAMPTZ,
    finished_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX uq_provisioning_jobs_idem ON provisioning_jobs(idempotency_key);
CREATE INDEX idx_provisioning_jobs_status ON provisioning_jobs(status);
```

---

### 2.5 `wrapper_files` — 文件代理映射（D4 重新签名方案）

```sql
CREATE TABLE wrapper_files (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES wrapper_tenants(id) ON DELETE CASCADE,

    -- 上游引用
    weknora_file_path   TEXT NOT NULL,        -- WeKnora "provider://..." 路径
    weknora_knowledge_id TEXT,                 -- 可选：来自哪条 knowledge
    weknora_tenant_id   BIGINT NOT NULL,

    -- 元数据快照
    file_name           TEXT,
    content_type        TEXT,
    size_bytes          BIGINT,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at          TIMESTAMPTZ,
    deleted_at          TIMESTAMPTZ
);

CREATE INDEX idx_wrapper_files_tenant ON wrapper_files(tenant_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_wrapper_files_path ON wrapper_files(weknora_file_path) WHERE deleted_at IS NULL;
```

> 包装层签名格式建议：`HMAC-SHA256(secret, file_id|expires|tenant_id)`。`secret` 与 WeKnora 的 HMAC 完全独立、不共享。

---

### 2.6 `audit_events` — 审计

```sql
CREATE TABLE audit_events (
    id              BIGSERIAL PRIMARY KEY,
    tenant_id       UUID,
    user_id         UUID,
    action          TEXT NOT NULL,            -- e.g. 'tenant.create', 'kb.delete', 'chat.rag', 'key.rotate'
    resource_type   TEXT,
    resource_id     TEXT,
    request_id      TEXT,
    status          TEXT,                     -- 'success' | 'failed'
    http_status     INT,
    weknora_status  INT,                      -- 上游状态码
    error_code      TEXT,
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_tenant_time ON audit_events(tenant_id, created_at DESC);
CREATE INDEX idx_audit_action_time ON audit_events(action, created_at DESC);
CREATE INDEX idx_audit_request ON audit_events(request_id);
```

**约束**
- 不记录敏感 body（API Key / 用户 query 全文 / 文件二进制）
- 可记录 query 的 `sha256(prefix)` 或长度 / 关键词命中数等可观测指标

---

### 2.7 `rate_limits`（可选）

```sql
CREATE TABLE rate_limit_buckets (
    key             TEXT PRIMARY KEY,         -- e.g. 'tenant:{id}:chat:rag'
    tokens          DOUBLE PRECISION NOT NULL,
    last_refill_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```
高频接口建议改用 Redis 实现，仅在落地需要持久化时用本表。

---

## 3. 数据关系图（简化）

```
wrapper_tenants ──1:1── weknora_credentials
        │
        ├──1:N── wrapper_users
        │
        ├──1:N── wrapper_files
        │
        ├──1:N── provisioning_jobs
        │
        └──1:N── audit_events
```

---

## 4. 迁移脚本管理

- 使用 [`golang-migrate/migrate`](https://github.com/golang-migrate/migrate) 或 `pressly/goose`，目录 `internal/store/migrations/`
- 文件命名：`0001_create_wrapper_tenants.up.sql` / `.down.sql`
- 不与 WeKnora 的 `migrations/` 共用 schema、共用 DB、共用连接池
- 默认连接同一 Postgres 实例的**不同 database**（如 `weknora` 与 `weknora_wrapper`）；亦可独立实例

---

## 5. 安全要点

| 项 | 措施 |
|---|---|
| API Key 解密粒度 | 仅在转发请求时解，请求结束立即丢弃 |
| 日志脱敏 | logger 注入 hook，匹配 `X-API-Key` / `Authorization` 字段全部替换 `***` |
| 数据库账户权限分级 | API 进程用读写账号；运维一次性任务用单独账号 |
| 备份加密 | DB 备份加密；KMS 密钥不与备份同位置 |
| KMS 密钥轮换 | DEK 按月轮换，KEK 半年轮换 |
| 跨网区域 | 包装层 DB 与 WeKnora DB 不要同库 |

---

## 6. 待澄清

- **多用户共享一个 WeKnora 租户**情况下，WeKnora 内部会话与消息的"归属用户"问题：目前所有动作都归同一个合成 owner；如果需要在 WeKnora 内做按外部用户的统计，需要在 `session.metadata` 或包装层端附加 `external_user_key` 字段，由包装层做聚合查询。
- API Key 过期机制：WeKnora `tenant.APIKey` 默认无过期；包装层是否要主动设置 `expires_at` 并定期轮换 → 建议**默认 90 天主动轮换**，可关闭。
- DEK / KEK 是否复用调用方业务系统的 KMS → 由部署方决定。
