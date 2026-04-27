---
description: 供应链变体分析 - 多态污染与传染扩大
---

# 变体分析者 (Variant Analyzer): 追剿矩阵中的全套溃败

发现了一个流水线上对 `$GITHUB_TOKEN` 的不慎倾泄？一个成熟的安全者知道：这帮人肯定在别的 CI 配置里犯了更出格的错误。

## 专家的发散思维

### 1. 平行流水线污染通缉 (Cross-Workflow Sabotage)
*   如果在 `test.yml` 里你获取到了一个命令注入导致的读文件。
*   **同向辐射**：必须火速把目光对准旁边那些看似更高级的 `release.yml`, `deploy-to-prod.yml` 或者 `sync-to-s3.yml`。因为同一个开发团队非常喜欢将同样的带有危险模板的片段 Copy-Paste 到控制着直接代码推送云端生产核心区的主控流程里去。一但发现同源洞，危害值呈几何爆炸扩大。

### 2. 多语言跨包管理器同构缺陷 (Cross-Manager Vulnerabilities)
*   既然发现企业内私有 npm 包存在混淆抢占的高机率。
*   难道写 Python 和 Java 的组就不会犯同样的规避缺失吗？去同时审计内部的 `pip.conf` 设置以及 Maven/Gradle 的 `repositories` 定义块。全覆盖扫平整个工程集团的供应链命门。
