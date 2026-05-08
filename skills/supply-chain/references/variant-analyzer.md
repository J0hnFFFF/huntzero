---
description: 供应链变体分析 - 多态污染与传染扩大
tags: [supply-chain, variant-analyzer, cross-workflow, cross-manager, copy-paste, blast-radius]
---

# 供应链变体分析 (Variant Analyzer): 追剿矩阵中的全套溃败

> 发现了一个流水线上对 secrets 的不慎倾泻？
> 一个成熟的安全者知道：这帮人肯定在别的 CI 配置里犯了更出格的错误。
> 你要的不是修复一个点，是绘制一张"同源漏洞分布图"。

## 变体分析核心方法论：同源漏洞辐射定律

**定律一：Copy-Paste 漏洞守恒定律**
> 如果开发者在一个 workflow 中使用了危险模式（如 `${{ github.event.* }}` 注入），
> 他们几乎一定在另一个 workflow 中复制了同样的模式。

**定律二：权限升级路径定律**
> 如果 `test.yml` 存在命令注入，攻击者可以窃取 secrets。
> 那些相同的 secrets 很可能在 `deploy.yml` 中被使用。
> test.yml 的漏洞 = deploy.yml 的突破口。

**定律三：跨生态同构缺陷定律**
> 如果 npm 生态存在 Dependency Confusion，
> 同一个公司的 Python/Java/Go 团队很可能犯了同样的配置错误。

---

## 变体分析一：平行流水线污染通缉 (Cross-Workflow Sabotage)

### 场景
在 `.github/workflows/test.yml` 中发现 `${{ github.event.issue.title }}` 表达式注入。

### 辐射检查清单

**Step 1：同源代码搜索**
在同一仓库的所有 workflow 中搜索相同或相似的危险模式：

```bash
# 搜索所有 workflow 中的表达式注入
 grep -r '\${{ github\.event\.' .github/workflows/
 
# 搜索所有 workflow 中的 pull_request_target
 grep -r 'pull_request_target' .github/workflows/
 
# 搜索所有 workflow 中 checkout PR head 的模式
 grep -r 'head\.sha\|head\.ref' .github/workflows/
```

**Step 2：权限升级路径分析**

绘制 workflow 之间的 secrets 流动图：

```
test.yml ──► 使用 secrets.AWS_KEY ──► 运行测试（低权限需求，高风险暴露）
    │
    ▼
deploy.yml ──► 使用 secrets.AWS_KEY ──► 部署到生产（高权限需求）
    │
    ▼
release.yml ──► 使用 secrets.GITHUB_TOKEN ──► 创建 release / 推送 tag
    │
    ▼
sync.yml ──► 使用 secrets.S3_BUCKET ──► 同步静态资源到 CDN
```

**关键洞察**：test.yml 的注入漏洞 → 窃取 AWS_KEY → 用 AWS_KEY 直接操作 deploy.yml 的目标环境。
攻击者甚至不需要攻破 deploy.yml，只需要攻破 test.yml 获得相同的凭证。

**Step 3：危险 workflow 优先级排序**

| Workflow 类型 | 典型权限 | 一旦攻破的后果 | 检查优先级 |
|--------------|---------|--------------|-----------|
| `release.yml` | GITHUB_TOKEN (写) | 可发布恶意 release、篡改 tag | P0 |
| `deploy.yml` | AWS/K8s/SSH | 可直接控制生产环境 | P0 |
| `publish.yml` | NPM/PyPI token | 可发布恶意包到公网 | P0 |
| `test.yml` | GITHUB_TOKEN (读) | 可窃取 secrets、横向移动 | P1 |
| `lint.yml` | 无 secrets | 影响较小 | P2 |

### 变体发现模板

```
## 同源漏洞变体报告

### 原始漏洞
- 位置: .github/workflows/test.yml:23
- 类型: CI 表达式注入
- 影响: 可窃取 secrets

### 辐射检查范围
- 扫描的 workflow 数量: [N]
- 发现的同源模式数量: [N]

### 变体详情
| # | Workflow | 位置 | 模式 | 权限 | 风险等级 |
|---|---------|------|------|------|---------|
| 1 | release.yml | L45 | `${{ github.event.release.body }}` | GITHUB_TOKEN(写) | Critical |
| 2 | deploy.yml | L12 | `pull_request_target` + checkout head | AWS_KEY | Critical |
| 3 | sync.yml | L78 | `${{ github.event.comment.body }}` | S3_TOKEN | High |

### 爆炸半径评估
- 直接受影响的 workflow: [N]
- 间接受影响的 workflow（共享 secrets）: [N]
- 预计完整修复需要修改的文件数: [N]
```

---

## 变体分析二：多语言跨包管理器同构缺陷 (Cross-Manager Vulnerabilities)

### 场景
在 npm 项目中发现了 Dependency Confusion（内部包未加 scope）。

### 辐射检查清单

**Step 1：识别组织中所有技术栈**

```
项目 A: Node.js (npm/yarn) ──► 发现 Dependency Confusion
项目 B: Python (pip/poetry) ──► 待检查
项目 C: Java (Maven/Gradle) ──► 待检查
项目 D: Go (go modules) ──► 待检查
项目 E: Rust (cargo) ──► 待检查
```

**Step 2：按技术栈检查同构缺陷**

| 技术栈 | Dependency Confusion 等价形式 | 检查方法 |
|-------|------------------------------|---------|
| Python | `pip.ini` / `pip.conf` 中 `--extra-index-url` 并行查询 | 检查是否有内部包名在 PyPI 上可被抢注 |
| Java | `pom.xml` 中 `<repositories>` 多源配置 | 检查 `mirrorOf` 是否意外覆盖内部源 |
| Go | `GOPRIVATE` 未设置，或 `go.mod` 中 `replace` 指向公网 | 检查内部模块路径是否在公网存在 |
| Rust | `Cargo.toml` 中 `registry` 未显式指定内部源 | 检查 crates.io 上是否有同名内部包 |
| Ruby | `Gemfile` 中 `source` 配置 | 检查 rubygems.org 上是否有同名内部 gem |

**Step 3：检查 CI/CD 配置的同构缺陷**

不同语言的 CI 配置可能使用相同的危险模式：

```yaml
# Node.js 项目中的危险模式
run: npm test --grep="${{ github.event.issue.title }}"

# Python 项目中的同构危险模式
run: pytest -k "${{ github.event.issue.title }}"

# Go 项目中的同构危险模式
run: go test -run "${{ github.event.issue.title }}"

# Java 项目中的同构危险模式
run: mvn test -Dtest="${{ github.event.issue.title }}"
```

**关键洞察**：如果开发者习惯在 CI 中使用 `"${{ github.event.* }}"` 模式，
这个习惯会跨语言、跨项目复现。

---

## 变体分析三：跨组织/跨生态的供应链污染

### 场景
攻击者发现目标公司使用了某个开源项目的 GitHub Action（如 `third-party/action@v1`）。

### 辐射检查

1. **Action 供应链检查**：
   - 这个 Action 的仓库是否使用 `pull_request_target`？
   - 这个 Action 的 `action.yml` 中是否有 `post` 步骤执行任意代码？
   - 这个 Action 是否被广泛使用？如果 Action 被攻破，影响多少项目？

2. **Composite Action 检查**：
   ```yaml
   # 目标项目使用的 composite action
   - uses: company/shared-actions/deploy@v1
   ```
   - `company/shared-actions` 仓库的 workflow 是否也有同样的漏洞？
   - 这个 shared action 被多少项目引用？

3. **Reusable Workflow 检查**：
   ```yaml
   - uses: company/.github/.github/workflows/reusable-test.yml@main
   ```
   - 可重用 workflow 中的漏洞 = 所有引用它的项目的漏洞

---

## 变体分析四：时间维度上的漏洞演变

### 场景
当前版本没有漏洞，但历史版本有。

### 检查清单

1. **历史 Commit 检查**：
   ```bash
   git log --all --source --full-history -S 'pull_request_target' -- '.github/workflows/'
   ```
   - 历史上是否曾经启用过 `pull_request_target`？
   - 是否有 secrets 曾经暴露在日志中（即使后来删除了 workflow）？
   - GitHub Action logs 是否仍可访问历史运行记录？

2. **历史 Release 检查**：
   - 已发布的版本中是否包含已被修复的漏洞？
   - 用户是否仍在使用旧版本？
   - 旧版本的 lockfile 是否指向已被接管的域名？

3. **依赖版本时间线**：
   - 项目依赖的某个包，其维护者是否最近更换了？
   - 是否有依赖包的版本在维护者被盗号后发布？
   - OSV 数据库中，该包的历史 CVE 时间线是否与项目使用版本重叠？

---

## 专家变体分析输出模板

```
## 变体分析报告

### 原始漏洞
[描述最初发现的漏洞]

### 辐射分析
- 同源模式扫描范围: [文件/目录范围]
- 发现的同源漏洞数量: [N]
- 跨语言/跨管理器检查: [已检查的技术栈列表]

### 变体清单
| 变体ID | 位置 | 漏洞类型 | 与原始的相似度 | 风险等级 | 修复优先级 |
|--------|------|---------|-------------|---------|-----------|
| VAR-01 | release.yml:45 | 表达式注入 | 完全相同 | Critical | P0 |
| VAR-02 | deploy.yml:12 | pull_request_target | 模式相同 | Critical | P0 |
| VAR-03 | pyproject.toml | Dependency Confusion | 跨语言同构 | High | P1 |

### 爆炸半径
- 直接影响: [N 个 workflow / N 个项目]
- 间接影响（共享 secrets/action）: [N 个 workflow / N 个项目]
- 用户影响: [N 个下游用户 / 整个生态]

### 修复建议
- 立即修复: [列出所有 P0 变体]
- 短期修复: [列出所有 P1 变体]
- 长期加固: [跨项目安全策略]
```
