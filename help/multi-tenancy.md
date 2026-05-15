# WeKnora 多租户架构指南

## 架构总览

```
用户注册 / OIDC 首次登录
       ↓
provisionOIDCUser() → Register() → CreateTenant()
       ↓
每个用户自动获得独立 Tenant（存储/LLM/检索配置全部隔离）
       ↓
MustTenantIDFromContext() — 注入每一层，强制数据隔离
```

---

## 一、租户生命周期

### 自动创建（默认行为）

WeKnora 采用 **"一用户一租户"** 模型，无需管理员手动创建：

- **表单注册**：`POST /api/v1/auth/register` → 自动调用 `CreateTenant()`
- **OIDC 首次登录**：`provisionOIDCUser()` → `Register()` → `CreateTenant()`

每个新用户注册后立即拥有一个隔离的独立租户，无需任何额外操作。

### 程序化创建（可选）

仅在需要通过脚本或 API 批量创建时使用：

| 方式 | 入口 |
|---|---|
| REST API | `POST /api/v1/tenants` |
| MCP Tool | `create_tenant`（MCP 服务内置工具） |

---

## 二、Settings 即是租户配置界面

前端所有 Settings 页面的作用域已自动绑定到**当前用户所属租户**，这就是 per-tenant 配置界面：

| Settings 页面 | 配置的租户资源 |
|---|---|
| Vector Store Settings | 当前租户的向量数据库连接 |
| LLM 模型配置 | 当前租户的 LLM 提供商与参数 |
| Web Search 配置 | 当前租户的网络搜索服务 |
| 检索配置 | 当前租户的 Retrieval 策略 |
| Chat History | 当前租户的会话保留策略 |

用户在 Settings 中的任何修改均只影响自己的租户，不会影响其他用户。

---

## 三、跨租户管理（默认关闭）

如需一个超级管理员账号能够查看和切换所有租户，须同时满足两个条件。

### 条件 1：服务端配置开关

```yaml
# config.yaml
tenant:
  enable_cross_tenant_access: true
```

### 条件 2：数据库授权指定用户

```sql
UPDATE users SET can_access_all_tenants = true WHERE email = 'admin@your-domain.com';
```

两个条件均满足后，该用户前端界面将出现 `TenantSelector` 组件，可搜索和切换所有租户。

> **实现位置**：`internal/handler/tenant.go` — `ListAllTenants` / `SearchTenants` 对上述两个条件执行双重校验，任一不满足均返回 403。

### 两个校验门（源码摘要）

```go
// Gate 1: 服务端配置
if !h.config.Tenant.EnableCrossTenantAccess {
    c.Error(errors.NewForbiddenError("Cross-tenant access is disabled"))
    return
}
// Gate 2: 用户权限字段
if !user.CanAccessAllTenants {
    c.Error(errors.NewForbiddenError("Insufficient permissions to access all tenants"))
    return
}
```

---

## 四、跨租户协作（Organization / Shared Space）

Standard 版本支持通过 Organization 跨租户共享知识库和 Agent：

- 角色体系：`owner` / `admin` / `editor` / `viewer`
- 可共享资源：Knowledge Base、Agent
- Lite 版本不包含此功能（`isLiteEdition()` 前端判断）

---

## 五、数据隔离机制

`MustTenantIDFromContext()` 在每一层强制注入 Tenant ID，覆盖所有数据访问路径：

- 存储层（向量数据库、文件存储）
- LLM 调用层
- Parser / DocReader 层
- 检索层
- Web Search 层

任何跨租户的数据泄露在架构层面均被阻断。

---

## 六、常见问题

**Q：为什么管理控制台没有"创建租户"入口？**  
A：注册即自动创建，无需手动操作。这是设计决策，不是功能缺失。

**Q：如何为某个租户单独配置 LLM 或向量数据库？**  
A：以该租户的用户账号登录，在 Settings 中配置即可，配置自动隔离。

**Q：如何查看系统内所有租户？**  
A：需启用 `enable_cross_tenant_access` 配置并对管理员账号设置 `can_access_all_tenants=true`，之后可通过 `GET /api/v1/tenants/all` 或前端 TenantSelector 查看。
