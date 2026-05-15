# 知识库权限管控指南

## 核心概念

| 概念 | 说明 |
|---|---|
| **Tenant**（租户） | 每个用户独立的私有工作区，KB 默认归属租户，租户间天然隔离 |
| **Organization**（协作空间） | 跨租户协作单元，可类比"部门"或"项目组" |
| **KnowledgeBaseShare** | KB → Organization 的授权记录，携带 permission 级别 |
| **OrgMemberRole** | `viewer` / `editor` / `admin`，三级权限，支持 `HasPermission()` 层级比较 |

---

## 一、Handler 层鉴权入口

所有知识库操作均经过 `validateKnowledgeBaseAccessWithKBID`（`internal/handler/knowledge.go:65`），按以下顺序走三条路径：

```
请求 → 提取 tenantID + userID
       ↓
[Path 1] kb.TenantID == tenantID?
         → YES → OrgRoleAdmin（所有者，最高权限，直接放行）
         ↓ NO
[Path 2] kbShareService.CheckUserKBPermission(kbID, userID)
         → isShared=true → 读取 sourceTenantID，返回对应 permission
         ↓ 无共享记录或用户不在任何含该 KB 的 org
[Path 3] agentShareService.UserCanAccessKBViaSomeSharedAgent(userID, tenantID, kb)
         → can=true → OrgRoleViewer（只读，通过共享智能体间接访问）
         ↓ 全部失败
         → 403 Forbidden
```

---

## 二、Service 层共享管理

### 创建共享（`kbshare.go:51`）

`ShareKnowledgeBase` 在写入前执行四项前置检查：

| 检查项 | 规则 |
|---|---|
| KB 归属 | `kb.TenantID == callerTenantID`，非所有者不可发起共享 |
| Org 存在 | `orgRepo.GetByID` |
| 成员资格 | 调用方必须是目标 org 成员 |
| 成员角色 | `member.Role.HasPermission(OrgRoleEditor)`，Viewer 无权共享 |

共享记录（`KnowledgeBaseShare`）关键字段：

```go
KnowledgeBaseID string        // 被共享的 KB
OrganizationID  string        // 目标 org
SourceTenantID  uint64        // KB 原始租户（用于跨租户 embedding 模型访问）
Permission      OrgMemberRole // viewer / editor / admin
SharedByUserID  string        // 操作人（用于后续撤权）
```

### 变更 / 撤销权限

- **`UpdateSharePermission`**：分享者本人 **或** 目标 org 的 Admin 均可操作
- **`RemoveShare`**：同上

---

## 三、批量检索时的权限穿透

搜索场景下，用户可能 `@` 引用跨租户知识，系统不直接拒绝，而是逐条鉴权：

**`GetKnowledgeBatchWithSharedAccess`**（`service/knowledge.go:295`）  
**`fetchKnowledgeDataWithShared`**（`service/knowledgebase_search_shared.go:34`）

```
批量 IDs
  → 先按当前 tenantID 拉取自有知识
  → 对未找到的 IDs：
      GetKnowledgeByIDOnly(id) → 获取 KnowledgeBaseID
      HasKBPermission(kbID, userID, OrgRoleViewer)
      → 有权 → 合并进结果集
      → 无权 → 静默跳过（不报错、不泄露存在性）
```

---

## 四、通过共享智能体的间接访问

`UserCanAccessKBViaSomeSharedAgent`（`service/agent_share.go:432`）支持用户通过被共享的智能体访问其绑定的知识库：

- 智能体配置中 `KBSelectionMode`：`none` / `all` / `selected`（含具体 KB ID 列表）
- 匹配则授予 **OrgRoleViewer**（只读，不可管理）

---

## 五、部门隔离：用 Organization 实现部门间数据隔离

### 为什么现有结构天然支持

`ListSharedKBsForUser`（`repository/kbshare.go:150`）的 SQL 核心：

```sql
JOIN organization_members ON organization_members.organization_id = kb_shares.organization_id
WHERE organization_members.user_id = ?
```

只返回"用户所在 org"的 KB。只要跨部门用户不在对方 org 内，所有访问路径均返回空或 403。

### 落地步骤（纯运维操作，无需改代码）

**第一步：每个部门建一个 Organization**

```http
POST /api/v1/organizations
{
  "name": "部门A",
  "require_approval": true
}
```

`require_approval: true` 防止成员自行加入其他部门的空间。

**第二步：将部门 KB 共享到本部门 Org**

```http
POST /api/v1/knowledge-bases/:kbID/share
{
  "organization_id": "deptA-org-id",
  "permission": "viewer"
}
```

**第三步：将部门人员加入对应 Org（不要跨部门添加）**

```http
POST /api/v1/organizations/:orgID/members
{
  "email": "user@company.com",
  "role": "viewer"
}
```

### 隔离生效原理

```
部门A用户访问部门B的 KB
  → Path 1: kb.TenantID ≠ 用户 tenantID  → 跳过
  → Path 2: CheckUserKBPermission         → 用户不在部门B的 org → isShared=false
  → Path 3: UserCanAccessKBViaSomeSharedAgent → 无共享智能体 → can=false
  → 403 Forbidden ✓
```

### 注意事项：Path 1 的行为

Path 1（`handler/knowledge.go:89`）对租户所有者**直接放行**，不走 org 检查：

```go
if kb.TenantID == tenantID {
    return kb, kbID, tenantID, types.OrgRoleAdmin, nil
}
```

在"一用户一租户"模型下，Path 1 通常只影响同一 tenant 内的多用户场景。如果同一部门的多名用户共享同一 tenant（非默认情况），他们之间的 KB 可见性由 tenant 边界决定，与 org 配置无关。

---

## 六、可选增强：代码层强制防误操作

若希望在代码层阻止管理员将 KB 误共享到错误部门，可为 `Organization` 添加 `Isolated` 标志：

**涉及改动（约 15 行）：**

1. `internal/types/organization.go` — 新增字段
```go
Isolated bool `json:"isolated" gorm:"default:false"`
```

2. `internal/application/service/kbshare.go` — `ShareKnowledgeBase` 内追加守卫
```go
if org.Isolated && kb.TenantID != tenantID {
    return nil, ErrNotKBOwner
}
```

3. 数据库迁移：`ALTER TABLE organizations ADD COLUMN isolated BOOLEAN DEFAULT FALSE`

启用后，`Isolated=true` 的 org 只允许其所有者租户的 KB 被共享进来，杜绝误操作。
