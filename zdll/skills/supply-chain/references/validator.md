---
description: 供应链验证器 - 定损：宣告企业数字化底盘的蒸发
tags: [supply-chain, validator, impact-assessment, critical, lateral-movement]
---

# 供应链验证器 (Validator): 衡量代码心脏的腐败辐度

> 这不是普通的 CVSS 打分。这是在衡量：一旦这个漏洞被利用，
> 这家公司的软件工厂是轻微受损，还是整栋大楼的地基被掏空。

## 定损框架：供应链漏洞的五个破坏维度

### 维度一：横向移动能力（Lateral Movement）

**核心问题**：攻击者从一个被攻破的节点，能到达多少其他节点？

| 场景 | 横向移动范围 | 评级 |
|------|------------|------|
| 单一项目的 CI Runner 被攻破，但 Runner 无网络外联、无共享资源 | 仅限该项目 | Low |
| CI Runner 持有 GITHUB_TOKEN，可修改其他仓库 | 整个组织 | High |
| CI Runner 持有 AWS Key，可访问云平台其他服务 | 整个云基础设施 | Critical |
| 被污染的 npm 包被多个内部项目依赖 | 整个公司代码库 | Critical |
| 被污染的 Docker base image 被所有微服务使用 | 整个生产环境 | Critical |

**关键检查点**：
- Runner 的 `GITHUB_TOKEN` 权限范围（repo 级 vs org 级）
- Secrets 是否是 org-level shared secrets
- 缓存是否是仓库级 / org 级共享
- 包是否是内部共享库（被多个项目依赖）

### 维度二：持久化能力（Persistence）

**核心问题**：攻击者需要持续维护攻击状态，还是一次投毒、长期生效？

| 持久化机制 | 维护成本 | 检测难度 | 典型场景 |
|-----------|---------|---------|---------|
| 修改源代码中的依赖版本 | 低（一次 PR） | 中 | Dependency Confusion |
| 污染 lockfile | 低（一次 PR） | 高 | Lockfile Poisoning |
| 污染共享缓存 | 低（一次构建） | 极高 | Build Cache Hijacking |
| 在 CI Runner 上植入后门 | 高（Runner 可能被回收） | 中 | CI Runner  compromission |
| 发布恶意包到公网 registry | 中（需要维护包存在） | 高 | Dependency Confusion |

**评级规则**：
- 持久化成本极低 + 检测难度极高 = **Critical 持久化威胁**
- 持久化成本低 + 检测难度高 = **High 持久化威胁**
- 需要持续维护 + 容易被检测 = **Medium 持久化威胁**

### 维度三：信任链断裂范围（Trust Chain Collapse）

**核心问题**：这个漏洞是否破坏了"用户信任开发者、开发者信任工具链"的基础假设？

| 断裂类型 | 影响描述 | 评级 |
|---------|---------|------|
| 单个依赖被替换 | 用户安装时中招，但范围可控 | Medium |
| 构建产物被篡改（无签名） | 用户下载的二进制不是开发者编译的 | High |
| 签名密钥泄露 | 攻击者可以伪造官方签名 | Critical |
| CI/CD 系统被完全控制 | 所有发布产物都不可信 | Critical |
| 包管理器本身被攻击 | 整个生态的信任基础崩塌 | Critical+ |

### 维度四：恢复成本（Recovery Cost）

**核心问题**：发现漏洞后，清理和恢复需要多少成本？

| 场景 | 恢复成本 | 评级 |
|------|---------|------|
| 回滚一个 PR，移除恶意依赖 | 数小时 | Low |
| 重新生成所有 lockfile 并审计 | 数天 | Medium |
| 清空所有共享缓存并重建 | 数天 + 构建时间损失 | Medium-High |
| 轮换所有泄露的 secrets（AWS Key、GitHub Token 等） | 数天到数周 | High |
| 通知所有下游用户更新 | 数周到数月 | High |
| 重建签名密钥 + 重新签名所有历史 release | 数月 | Critical |
| 无法确定污染范围（缓存投毒，不知道哪些产物被感染） | 不可估量 | Critical |

### 维度五：攻击触发门槛（Exploitation Barrier）

**核心问题**：攻击者利用这个漏洞需要付出多少成本？

| 门槛 | 描述 | 对评级的影响 |
|------|------|------------|
| 零交互（0-Click） | 用户正常安装/构建即中招，无需任何额外操作 | 评级 +1 |
| 低交互 | 需要用户执行一次常规操作（如 `npm install`） | 评级不变 |
| 中等交互 | 需要用户接受 PR、点击链接、或执行非标准命令 | 评级 -0.5 |
| 高门槛 | 需要内部人员配合、或绕过额外的安全控制 | 评级 -1 |

## 专家评估标准

### 场景一：代码装配线凭据最高机密倒灌

**触发条件**：通过污染 PR 或模板注入在 CI Runner 中执行木马命令，获取 `GITHUB_TOKEN`（写权限）或部署凭证。

**五维度评估**：
- 横向移动：GITHUB_TOKEN 可写整个组织 → **Critical**
- 持久化：Token 可长期有效（除非被手动 revoke）→ **High**
- 信任链：官方仓库可被直接修改 → **Critical**
- 恢复成本：需轮换所有 secrets + 审计所有修改 → **Critical**
- 触发门槛：只需提交 PR / Issue → **Low 门槛**（评级 +1）

**综合定损**：**Critical / P0**

**威慑声明**：
> 这不只宣告应用的死刑。攻击者可以借这台母机随意在生产机上铺设挖矿木马，自由擦除、篡改这个组织的所有源代码库文件。这叫底盘全灭。

### 场景二：全体开发与下游末端用户的投喂感染

**触发条件**：利用 Dependency Confusion 或恶意 npm 包的 `postinstall` 脚本。

**五维度评估**：
- 横向移动：所有安装该包的开发者和用户 → **Critical**
- 持久化：恶意包在 registry 中长期存在 → **High**
- 信任链：用户信任的包管理器分发了恶意代码 → **Critical**
- 恢复成本：需通知所有用户更新 + 公网 registry 下架 → **High**
- 触发门槛：`npm install` 即中招 → **0-Click**（评级 +1）

**综合定损**：**Critical / P0**

**威慑声明**：
> 这将感染全公司、全球所有刚刚敲下 `npm install` 的技术人员，以及集成发布出海的客户安装包。这等同于拿下一场大范围零日 APT 战争的首杀。

### 场景三：Lockfile 隐蔽投毒

**触发条件**：通过 PR 修改 lockfile，将深层依赖指向恶意 URL。

**五维度评估**：
- 横向移动：取决于被投毒的依赖被多少项目使用 → **Medium-High**
- 持久化：lockfile 被合并后长期有效 → **High**
- 信任链：开发者信任的 lockfile 机制被破坏 → **High**
- 恢复成本：需审计所有 lockfile 历史 → **High**
- 触发门槛：需要 PR 被合并 → **Medium 门槛**

**综合定损**：**High / P1**

### 场景四：Build Cache 污染

**触发条件**：攻击者获取 runner 执行权，篡改共享缓存。

**五维度评估**：
- 横向移动：所有使用该缓存的 workflow → **High**
- 持久化：缓存长期存在，直到被手动清除 → **High**
- 信任链：CI 系统的缓存机制不可信 → **High**
- 恢复成本：可能无法确定哪些构建使用了污染缓存 → **Critical**
- 触发门槛：需要先获取 runner 执行权 → **High 门槛**（评级 -1）

**综合定损**：**High / P1**（但如果有迹象表明污染已发生 → **Critical**）

## 定损输出模板

```
## 漏洞定损报告

### 基本信息
- 漏洞类型: [Dependency Confusion / Install Execution / Lockfile Poisoning / CI Poisoning / Cache Hijacking]
- 发现位置: [文件路径]
- 攻击入口: [PR / Issue / 包安装 / CI 触发]

### 五维度评估
| 维度 | 评级 | 理由 |
|------|------|------|
| 横向移动 | [Critical/High/Medium/Low] | |
| 持久化 | [Critical/High/Medium/Low] | |
| 信任链断裂 | [Critical/High/Medium/Low] | |
| 恢复成本 | [Critical/High/Medium/Low] | |
| 触发门槛 | [0-Click/Low/Medium/High] | |

### 综合定损
- 严重等级: [Critical / High / Medium / Low]
- 优先级: [P0 / P1 / P2 / P3]
- 业务影响: [单项目 / 多项目 / 全组织 / 全用户生态]

### 最坏情况推演
[描述漏洞被利用后的 72 小时内可能发生什么]

### 修复紧迫性
- 立即行动: [如：rotate secrets / disable workflow / unpublish package]
- 短期修复: [如：添加 scope / 使用 npm ci / 审查 lockfile]
- 长期加固: [如：签名机制 / 缓存校验 / 最小权限原则]

### OSV 协同（如适用）
- 已知 CVE 是否提高了该漏洞的利用概率或影响范围？
- OSV 报告的 affected version 是否与本项目匹配？
```
