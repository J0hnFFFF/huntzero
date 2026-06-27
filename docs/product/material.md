# Material: zerodayFind 产品介绍 PPT

## 1. Overview
- zerodayFind 是一个 LLM 驱动的 Hive-Mind 代码安全审计引擎
- 核心创新：用 AI 模拟安全研究员的认知过程（理解→假设→验证→审查），而非传统规则匹配
- 三种产品形态：CLI 扫描工具、CI/CD 集成组件、代码审计服务

## 2. Background
- 传统代码审计工具（SAST/DAST）基于规则匹配，无法发现新型漏洞
- 市场上主流工具：Semgrep、CodeQL、SonarQube、Checkmarx 等均为静态规则引擎
- LLM 在漏洞检测领域的研究快速增长（2025-2026），多篇学术论文验证可行性
- CI/CD 安全扫描已成为 DevSecOps 标准实践，SARIF 是工业界标准报告格式
- 企业对"零日漏洞"内部发现的重视度持续上升

## 3. Key Info
- Cerebrum 四层认知模型：DEEP UNDERSTANDING → ADVERSARIAL EMPATHY → ABDUCTIVE REASONING → EPISTEMOLOGICAL HUMILITY
- 五层语义去重管道：Bloom 预过滤 → 锚点匹配 → 结构指纹 → Bigram Jaccard → Unigram Jaccard → 字符3-gram
- 贝叶斯置信度传播：confidence = 0.4×自身 + 0.6×父假设×因果强度
- 8 种 Drone 角色：evidence-collector、data-flow-tracer、state-validator、topology-mapper、exploit-crafter、harness-generator、crash-analyzer、scope-definer
- 12 个安全领域检测：web、ai-agent、binary、storage-engine、data-parser、desktop、infra、foundation-lib、supply-chain、mail、browser、counter
- SARIF 2.1.0 标准输出 + 基线比对 + --fail-on 严重性策略门控
- 支持 7 种语言的 TreeSitter 语义分析
- Go 单二进制分发，无需 Python 环境

## 4. Evidence
- Case: 内部测试 - 对比传统工具与 zdll 在同一项目上的发现差异（假设数据）
- Case: CI 模式 - 集成到 GitHub Actions/GitLab CI 的完整流水线
- Case: SARIF 报告 - VS Code SARIF Viewer 直接查看扫描结果

## 5. Analysis
- 传统工具痛点：规则库滞后、误报率高、无法发现逻辑类漏洞、需要安全专家配置规则
- zdll 优势：溯因推理而非模式匹配、领域自适应知识、贝叶斯置信度避免误报、CI/CD 无缝集成
- 不足：需要 LLM API 调用（成本）、依赖高质量模型推理能力、首次扫描冷启动较慢
- 与 Python 版的关系：Go 版侧重 CLI/CI 场景，Python 版侧重服务化/集群场景

## 6. Outlook
- AI Agent 在安全领域的应用趋势持续上升
- SARIF 成为 CI/CD 安全报告的工业标准
- DevSecOps 从"检查清单"向"智能推理"演进
- 多模型协作（Hive-Mind）是未来安全审计的核心范式

## Summary
- High-authority: 3 (学术论文 + SARIF标准 + DevSecOps实践)
- Gaps: 缺少真实客户案例数据（需标注假设数据）
