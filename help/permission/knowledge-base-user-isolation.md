# 不同用户之间的知识库隔离与共享配置

## 结论

当前工程中，不同用户之间的知识库内容隔离主要依靠 **租户边界** 完成；跨用户共享依靠 **Organization 共享空间** 完成。

推荐使用方式是：

1. 每个用户保持独立租户。
2. 需要共同访问的知识库，通过 Organization 共享给共同成员。
3. 只属于某个用户的知识库，不共享到包含其他用户的 Organization。

不要为了实现多人协作而把多个用户直接绑定到同一个 `tenant_id`。如果多个用户共享同一租户，他们会天然拥有该租户内知识库的所有者级访问能力，无法表达“用户 A 可见但用户 B 不可见”的细粒度隔离。

## 当前隔离机制

### 1. 租户隔离

知识库表以 `tenant_id` 标识归属租户。请求进入系统后，鉴权中间件会从 JWT 用户或 `X-API-Key` 中解析出当前租户，并把租户信息注入请求上下文。

后续知识库访问会先判断：

```go
if kb.TenantID == tenantID {
    return kb, kbID, tenantID, types.OrgRoleAdmin, nil
}
```

也就是说，只要知识库归属于当前请求租户，当前用户会被视为该知识库的所有者，拥有最高权限。

相关实现：

- `internal/middleware/auth.go`
- `internal/handler/knowledgebase.go`
- `internal/handler/knowledge.go`

### 2. Organization 共享

当知识库不属于当前租户时，系统会检查该知识库是否被共享到了某个 Organization，并且当前用户是否是该 Organization 的成员。

共享记录由 `kb_shares` 表表示，核心字段包括：

| 字段 | 说明 |
|---|---|
| `knowledge_base_id` | 被共享的知识库 |
| `organization_id` | 目标共享空间 |
| `source_tenant_id` | 知识库原始租户 |
| `permission` | 共享权限：`viewer` / `editor` / `admin` |
| `shared_by_user_id` | 发起共享的用户 |

用户是否能访问共享知识库，取决于：

1. 知识库是否共享到了某个 Organization。
2. 当前用户是否是该 Organization 成员。
3. 共享权限和成员角色共同决定最终权限。

最终权限取“共享权限”和“用户在 Organization 中角色”的较低值。例如：知识库共享权限是 `editor`，但用户在空间中是 `viewer`，则用户最终只能只读访问。

相关实现：

- `internal/application/service/kbshare.go`
- `internal/application/repository/kbshare.go`
- `internal/types/organization.go`

### 3. 共享智能体的间接访问

系统还支持通过共享 Agent 间接访问知识库。如果某个共享 Agent 绑定了某知识库，用户可能通过该 Agent 获得该知识库的只读访问能力。

因此，如果某个知识库需要严格对某用户不可见，不仅不要直接共享该知识库，也不要把绑定了该知识库的 Agent 共享给该用户所在的 Organization。

相关实现：

- `internal/application/service/agent_share.go`

## 目标场景

希望实现：

1. 用户 A 与用户 B 都可以访问知识库 `X`。
2. 用户 A 可以访问知识库 `Xa`，但用户 B 不可以访问。
3. 用户 B 可以访问知识库 `Xb`，但用户 A 不可以访问。

推荐配置如下：

| 知识库 | 所属租户 | 共享到 Organization | 用户 A | 用户 B |
|---|---|---|---|---|
| `X` | A 或 B 的租户 | `AB共享空间` | 可访问 | 可访问 |
| `Xa` | A 的租户 | 不共享到包含 B 的空间 | 可访问 | 不可访问 |
| `Xb` | B 的租户 | 不共享到包含 A 的空间 | 不可访问 | 可访问 |

## 配置步骤

### 1. 保持 A、B 为独立租户

分别注册用户 A 和用户 B，让系统按默认流程为每个用户创建独立租户。

不要手工把 A、B 的 `users.tenant_id` 改成同一个值，否则 A 和 B 会共享同一租户内所有知识库。

### 2. 创建共享空间

由 A 或 B 创建一个 Organization，例如：

```http
POST /api/v1/organizations
Content-Type: application/json

{
  "name": "AB共享空间",
  "description": "用户A和用户B共同访问知识库X",
  "require_approval": true,
  "searchable": false
}
```

建议：

- `require_approval: true`，避免其他用户自行加入。
- `searchable: false`，避免该空间被无关用户发现并申请加入。

### 3. 将 A、B 加入共享空间

可以通过控制台的共享空间成员管理添加，也可以调用接口：

```http
POST /api/v1/organizations/{orgID}/invite
Content-Type: application/json

{
  "user_id": "user-b-id",
  "role": "viewer"
}
```

如果 B 只需要读取 `X`，角色用 `viewer` 即可。如果 B 需要维护 `X` 中的内容，可以设置为 `editor`。

### 4. 创建共同知识库 X

由 A 或 B 创建知识库 `X`。

假设由 A 创建，则 `X` 归属 A 的租户。B 默认不可见，必须通过 Organization 共享后才能访问。

### 5. 将 X 共享到 AB共享空间

在控制台中打开知识库 `X` 的共享入口，选择 `AB共享空间`，设置权限：

- `viewer`：B 只能查看、搜索、问答。
- `editor`：B 可以编辑共享知识库内容。

对应接口：

```http
POST /api/v1/knowledge-bases/{kbID}/shares
Content-Type: application/json

{
  "organization_id": "{orgID}",
  "permission": "viewer"
}
```

### 6. 创建 A 私有知识库 Xa

由 A 创建知识库 `Xa`，并且不要共享到任何包含 B 的 Organization。

这样：

- A 因为是 `Xa` 所属租户用户，可以访问。
- B 因为不属于 A 的租户，且没有 Organization 共享记录，不能访问。

### 7. 创建 B 私有知识库 Xb

由 B 创建知识库 `Xb`，并且不要共享到任何包含 A 的 Organization。

这样：

- B 因为是 `Xb` 所属租户用户，可以访问。
- A 因为不属于 B 的租户，且没有 Organization 共享记录，不能访问。

## 权限判定顺序

知识库访问时大致按以下顺序判断：

```text
请求
  |
  v
获取当前 tenant_id 与 user_id
  |
  v
知识库 tenant_id 是否等于当前 tenant_id？
  |-- 是：所有者访问，授予 admin
  |
  |-- 否：
        |
        v
      是否通过 Organization 共享给当前用户？
        |-- 是：授予共享权限与成员角色的较低权限
        |
        |-- 否：
              |
              v
            是否可通过共享 Agent 间接访问？
              |-- 是：授予 viewer
              |-- 否：拒绝访问
```

## 注意事项

### 不要使用同租户多用户来做该场景

虽然数据模型允许多个用户指向同一个 `tenant_id`，但当前产品设计默认是一用户一租户。知识库权限的第一判断是租户归属，同租户用户会直接获得所有者级访问。

如果 A、B 共用一个租户，则：

- `X` 可被 A、B 访问。
- `Xa` 也会被 A、B 访问。
- `Xb` 也会被 A、B 访问。

这不符合“Xa 仅 A 可见、Xb 仅 B 可见”的要求。

### 私有知识库不要绑定到共享 Agent

如果 `Xa` 被某个 Agent 绑定，而该 Agent 被共享给包含 B 的 Organization，B 可能通过共享 Agent 获得对 `Xa` 的只读访问。

因此，私有知识库需要同时检查：

1. 是否被直接共享到 Organization。
2. 是否被某个共享 Agent 间接暴露。

### 共享空间成员要谨慎维护

Organization 是共享授权的边界。只要用户加入了某个 Organization，就可能访问共享到该 Organization 的知识库。

建议按访问范围拆分 Organization：

- 共同知识库使用一个公共共享空间。
- 部门知识库使用部门共享空间。
- 私有知识库不进入任何共享空间。

## 最小配置示例

```text
用户 A：独立注册，租户 tenant_A
用户 B：独立注册，租户 tenant_B

Organization：AB共享空间
成员：
  - 用户 A：admin
  - 用户 B：viewer

知识库：
  - X：由 A 创建，共享到 AB共享空间，permission=viewer
  - Xa：由 A 创建，不共享
  - Xb：由 B 创建，不共享

结果：
  - A 可访问 X、Xa
  - B 可访问 X、Xb
  - A 不可访问 Xb
  - B 不可访问 Xa
```
