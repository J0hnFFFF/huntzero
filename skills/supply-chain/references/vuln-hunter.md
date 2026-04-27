---
description: 供应链漏洞挖掘 - 构建与分发链的第一性原理追踪
---

# 供应链漏洞挖掘 (Supply Chain Vuln Hunter)

超越概念演示，追踪真实可利用的供应链攻击链。

## 核心分析框架

### 原理一：依赖解析算法的不确定性
**关键问题**：给定相同的清单文件（package.json/go.mod/pom.xml），在不同环境/时间点解析出的依赖树是否相同？

**追踪路径**：
- 分析包管理器的版本范围解析算法（semver `^`, `~`, `*`, `>=`）
- 检查多源优先级：当私有源和公共源同时存在同名包时，胜出者是谁？
- 检查锁文件的完整性校验：`integrity` 字段是否被解析器强制验证
- 分析 `overrides` / `resolutions` / `replace` 指令能否被滥用
- **核心方法**：在依赖解析代码中搜索"版本比较"和"源选择"逻辑，寻找可被外部影响的决策点
- 验证：能否通过在公共源发布特定版本号的包，劫持目标项目的构建？

### 原理二：安装时代码执行的隐式信任
**关键问题**：`npm install` / `pip install` / `cargo build` 到底执行了哪些代码？用户知道吗？

**追踪路径**：
- **npm**: `preinstall`, `postinstall`, `prepare` 生命周期脚本的执行条件
- **pip**: `setup.py` 中的 `cmdclass` 自定义、`pyproject.toml` 的 build backend 任意代码执行
- **cargo**: `build.rs` build script 的执行权限和沙箱状态
- **Maven**: 插件的 `<execution>` 阶段绑定 — `process-resources` 阶段的任意代码执行
- 检查是否有 flag/配置 可以禁用安装时脚本执行（如 `--ignore-scripts`），以及默认是否禁用
- 验证：安装一个包是否等同于执行该包作者的任意代码？

### 原理三：CI/CD 管线的权限过度授予
**关键问题**：CI/CD Runner 执行不可信代码时，拥有什么权限？secrets 暴露了吗？

**追踪路径**：
- **GitHub Actions**: `pull_request_target` 触发器为外部 PR 提供 write 权限 + secrets
- **GitHub Actions**: `${{ github.event.issue.title }}` 等表达式注入 → 任意命令执行
- **GitLab CI**: `include` 指令引用外部 YAML — YAML 注入/合并键覆盖
- **Jenkins**: `Jenkinsfile` 在沙箱外执行的 Groovy 代码路径
- 分析 workflow 中每一步的环境变量——哪些包含 secrets？哪些步骤的 stdout 被公开？
- **核心方法**: 搜索所有 `.github/workflows/*.yml`, `.gitlab-ci.yml`, `Jenkinsfile` 中的表达式/变量引用
- 验证：外部 PR 作者能否通过修改 workflow 文件或 test 代码获取 secrets？

### 原理四：构建产物完整性链的断裂
**关键问题**：从源码到用户手上的二进制，中间经过了多少个可被篡改的环节？

**追踪路径**：
- 分析 release 签名流程：是否有 Sigstore/GPG 签名？签名密钥存放在哪？
- 检查版本 tag 和 commit 的绑定强度：tag 能否被移动到另一个 commit？
- 分析 Docker 镜像构建：base image 是否 pin 了 digest？`latest` 标签可被替换
- 检查 CDN 分发：release 下载 URL 是否可被路径穿越或 cache 污染
- 验证：能否在不修改源码仓库的情况下，替换最终分发到用户的二进制？

### 原理五：依赖间的传递性信任
**关键问题**：你信任 A，A 依赖 B，B 依赖 C。C 的维护者被盗号了。你受影响吗？

**追踪路径**：
- 绘制依赖树，标注每个依赖的维护者数量、最后更新时间、下载量
- 识别"关键单点"：只有一个维护者 + 高下载量 + 没有 2FA 的依赖
- 检查 lockfile 中的 `resolved` URL 是否指向可被接管的域名
- 分析 typosquatting 风险：是否有常见拼写错误的同名包已被注册
- 验证：依赖树中是否存在"一人被盗号 → 影响整条链"的单点？

## 高命中率漏洞 Pattern

| Pattern | 检测方法 | 典型目标 |
|---------|---------|---------|
| GitHub Actions 表达式注入 | 搜索 `${{ github.event.*.title }}` 等用户可控表达式 | 任何开源项目 |
| `pull_request_target` + checkout PR | 搜索 workflow 中 `pull_request_target` + `actions/checkout` PR head | 任何开源项目 |
| npm postinstall RCE | 检查 package.json 的 `scripts.postinstall` | npm 生态 |
| setup.py 任意执行 | 检查 `setup.py` 中 `subprocess.call` / `os.system` | PyPI 生态 |
| Lockfile 域名接管 | 提取 lockfile 中所有 `resolved` URL 的域名，检查 DNS 状态 | 所有 |
| Docker FROM latest | 检查 Dockerfile 的 `FROM` 是否使用 digest 或精确版本 | 所有 |

## 输出格式

```
## 发现报告

### 原理映射
- 触发原理: [解析不确定性/安装时执行/CI权限泄露/产物完整性/传递信任]
- 攻击链: [具体的从入口到影响的路径]

### 脆弱路径
- 入口: [PR/Issue/包发布/CI配置]
- 攻击者控制点: [哪个文件/字段/参数]
- 最终影响: [窃取secrets/篡改构建/植入后门]

### 验证思路
[如何构造最小 PoC 触发 — 使用无害payload如echo/curl]

### 危害本质
- 影响范围: [单个项目/所有使用者/整个生态]
- 持续性: [一次性/持久化]
```
