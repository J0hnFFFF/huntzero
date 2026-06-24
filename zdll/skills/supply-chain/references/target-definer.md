---
description: 供应链目标定义 - 专家看到构建蓝图时的第一直觉
tags: [supply-chain, target-definer, dependency-confusion, ci-cd, lockfile]
---

# 供应链目标定义 (Target Definer): 专家看到构建蓝图时的第一直觉

> 在供应链领域，你看到的不应该只是"这是一组源代码"。
> 你应该看到：原材料从哪来？工厂机器是谁在操作？成品在离开车间前经过了多少不可信的搬运工？

## 触发器：什么景象让供应链专家立刻警觉？

### 触发器一：看到一个内部包名未加 scope / prefix

**你的第一眼反应**："这包名在公网是不是可以随便注册？"

看到一个引用叫 `"auth-utils": "^1.2.0"` 或 `"common-lib": ">=2.0"`，没有 `@company/` 前缀，没有内网域名锁定——这就是 Dependency Confusion 的温床。

**立即追问链**：
1. 这个包名在 npmjs.com / PyPI / crates.io / Maven Central 上是否已被注册？
2. 包管理器的源优先级是什么？`registry` 配置里是否同时存在公共源和私有源？
3. 版本号解析规则是什么？如果公网出现更高版本号（如 `999.0.0`），包管理器会选哪个？
4. 是否有 `--extra-index-url`（pip）、`registry.npmjs.org` fallback（npm）、`mirrors`（Maven）等机制会静默交叉查询？
5. 私有包的版本号策略是什么？是否内部用的版本范围（如 `^1.0.0`）在公网上可以被恶意 `2.0.0` 覆盖？

### 触发器二：看到 `.github/workflows/` 或 CI/CD 配置

**你的第一眼反应**："这个流水线的触发条件和执行权限之间的张力，是不是可以压垮安全边界？"

看到 `on: pull_request` + `secrets.GITHUB_TOKEN` + `npm install` 组合——这是 CI Poisoning 的经典信号。

**立即追问链**：
1. 触发器是什么？`pull_request` vs `pull_request_target`？后者给外部 PR 提供写权限 + secrets 访问
2. 代码 checkout 的是哪个 ref？`refs/pull/xx/merge` 还是 PR 作者的 fork 分支？
3. Runner 执行环境里有什么 secrets？AWS Key？Docker Hub Token？Kubeconfig？SSH Key？
4. 是否有步骤把用户可控输入直接注入到 shell 命令中？例如 `run: npm test --grep="${{ github.event.issue.title }}"`
5. 构建产物是否有签名？release artifact 是否经过 hash 校验？谁持有签名密钥？

### 触发器三：看到锁文件（lockfile）出现在变更中

**你的第一眼反应**："这动辄几千行的 diff，有多少行是真的被人类审查过的？"

看到 `package-lock.json` 有 3000+ 行变更，或 `go.sum` / `Pipfile.lock` 被修改——这是 Lockfile Poisoning 的信号。

**立即追问链**：
1. lockfile 中是否有新的 `resolved` URL 指向非官方域名？
2. `integrity` 哈希值是否与已知的包版本对应？（可交叉验证）
3. 变更是否集中在某个深层传递依赖上？那可能是刻意挑选的隐蔽目标
4. lockfile 是否被 CI 强制校验？例如 `npm ci`（校验 lockfile 严格匹配）vs `npm install`（可随意修改）
5. 这个 lockfile 变更的来源是人工提交还是自动化 bot？自动化来源的信任锚点在哪？

### 触发器四：看到生命周期脚本（lifecycle scripts）

**你的第一眼反应**："安装这个包的时候，什么代码会无条件执行？"

看到 `package.json` 中的 `"preinstall"`、`"postinstall"`、`"prepare"`，或 `setup.py` 中的 `cmdclass` 修改，或 Maven plugin 的 `process-resources` 阶段绑定——这是 Execution as Install Side-Effect 的信号。

**立即追问链**：
1. 这个生命周期脚本执行了什么？是否有网络请求？是否有子进程调用？
2. 包管理器是否提供禁用脚本的 flag？（如 npm 的 `--ignore-scripts`，pip 的 `--no-build-isolation` 是反例）
3. 默认安装行为下，脚本是否会被执行？（npm 默认执行，cargo 的 `build.rs` 默认执行，pip 的 `setup.py` 默认执行）
4. 这个脚本所在的包处于依赖树的什么位置？是顶层依赖还是深层传递依赖？
5. 攻击者是否只需要控制一个四级传递依赖就能通过这个钩子实现 RCE？

### 触发器五：看到缓存配置或共享构建产物

**你的第一眼反应**："这个缓存被污染后，有多少后续构建会中招？"

看到 CI 中 `actions/cache` 配置、`.npm` 全局缓存、`target/` 目录复用、Docker layer cache——这是 Build Cache Hijacking 的信号。

**立即追问链**：
1. 缓存的 key 是什么？是否仅基于 lockfile hash？攻击者能否影响 key 的生成？
2. 缓存恢复后是否有完整性校验？还是直接信任并使用？
3. 缓存是 runner-local 还是跨 runner 共享？（跨 runner = 影响面更大）
4. 攻击者如果短暂获取 runner 执行权，能否篡改缓存内容而不破坏当前构建？
5. 缓存中是否包含编译后的二进制或 node_modules？这些产物是否经过签名？

## 攻击面地图：供应链的全链条审计清单

| 链条环节 | 目标文件/目录 | 审计焦点 |
|---------|-------------|---------|
| **依赖声明** | `package.json`, `requirements.txt`, `go.mod`, `pom.xml`, `build.gradle`, `Cargo.toml`, `Pipfile` | 包名 scope、版本范围宽松度、私有源配置、多源优先级 |
| **依赖锁定** | `package-lock.json`, `yarn.lock`, `Pipfile.lock`, `go.sum`, `Cargo.lock`, `poetry.lock` | resolved URL、integrity hash、变更来源、校验严格度 |
| **CI/CD 配置** | `.github/workflows/*.yml`, `.gitlab-ci.yml`, `Jenkinsfile`, `.circleci/config.yml` | 触发器权限、secrets 暴露面、表达式注入、checkout ref |
| **构建脚本** | `Makefile`, `build.sh`, `Dockerfile`, `docker-compose.yml` | base image 固定度、构建时网络请求、多阶段构建完整性 |
| **包元数据** | `setup.py`, `setup.cfg`, `pyproject.toml`, `package.json` scripts | 生命周期钩子、build backend、自定义 install 逻辑 |
| **分发产物** | release assets, Docker Hub tags, npm package tarball | 签名机制、tag 可移动性、CDN 分发完整性 |

## 输出要求

当你完成目标定义后，必须输出以下内容：

1. **攻击面拓扑图**：用文本或 ASCII 画出 `源码提交 → 依赖拉取 → 构建执行 → 产物分发` 的全链条，标记每个环节的信任边界
2. **高危信号清单**：列出这个项目最可能中招的 3-5 个第一性原理（从五大原理中选取），每个信号附带具体的文件路径和配置片段
3. **OSV 上下文融合**：如果 OSV 扫描报告已知 CVE，检查受影响的包是否出现在关键路径上（如 CI 触发器附近的依赖、生命周期脚本中的依赖），并标记"已知漏洞 + 攻击面重叠 = 高危"
