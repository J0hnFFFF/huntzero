# PPT Outline

## Overview
zerodayFind 产品介绍 PPT — 定位为 CI/CD 安全组件、代码审计工具和代码审计服务。核心信息是：zerodayFind 是 LLM 驱动的认知型代码安全审计引擎，用 AI 模拟安全研究员的思维过程，从假设出发溯因推理发现真实漏洞，比传统规则扫描器发现更多逻辑类漏洞和零日风险。

## Outline Content

## Page 1: Cover - 产品封面
- **Page Type**: Cover
- **Page Title**: zerodayFind
- **Page Subtitle**: LLM 驱动的认知型代码安全审计引擎
- **Content Structure**:
  - 主标题: zerodayFind
  - 副标题: LLM 驱动的认知型代码安全审计引擎
  - 产品标签: CLI 工具 · CI/CD 组件 · 审计服务
  - 简要说明: 用 AI 模拟安全研究员的思维过程，从假设出发，溯因推理发现真实漏洞

## Page 2: TOC - 目录导航
- **Page Type**: TOC
- **Page Title**: 目录
- **Content Structure**:
  - 章节1: 传统审计困境 — 规则匹配的局限
  - 章节2: 产品介绍 — Hive-Mind 认知审计引擎
  - 章节3: 产品形态 — CLI · CI/CD · 审计服务
  - 章节4: 技术亮点 — 去重·置信度·领域知识
  - 章节5: 对比优势 — 为什么不同

## Page 3: Transition - 传统审计困境
- **Page Type**: Transition
- **Page Title**: 传统审计困境
- **Page Subtitle**: 规则匹配无法发现未知漏洞

## Page 4: Content - 传统工具三大痛点
- **Page Type**: Content
- **Page Title**: 规则扫描器的三大痛点
- **Content Structure**:
  - 痛点1: 规则库滞后 — 只能检测已知模式，新漏洞类型出现后需人工编写规则，响应周期长达数周至数月
  - 痛点2: 误报率居高不下 — 传统 SAST 工具误报率普遍 30-60%，真实漏洞淹没在噪音中
  - 痛点3: 无法发现逻辑类漏洞 — 权限绕过、业务逻辑缺陷需要理解代码意图，规则引擎做不到
  - 数据: 67%的安全团队认为现有工具不足以发现关键漏洞；误报率平均 42%

## Page 5: Content - LLM时代的审计革命
- **Page Type**: Content
- **Page Title**: 从模式匹配到认知推理
- **Content Structure**:
  - 传统方式: 规则引擎扫描代码 → 匹配已知模式 → 产生告警 → 人工甄别
  - 认知审计方式: LLM理解代码意图 → 溯因推理形成假设 → 验证假设 → Critic审查质量
  - 核心差异: 从"代码是什么"到"代码做什么" — 安全审计从语法层面升级到语义层面
  - LLM 不替代专家，而是放大专家认知能力

## Page 6: Transition - 产品介绍
- **Page Type**: Transition
- **Page Title**: 产品介绍
- **Page Subtitle**: Hive-Mind 认知审计引擎

## Page 7: Content - 产品定位总览
- **Page Type**: Content
- **Page Title**: 三位一体产品定位
- **Content Structure**:
  - 定位1: CLI 审计工具 — Go 单二进制分发，一行命令扫描项目
  - 定位2: CI/CD 安全组件 — SARIF 标准输出，基线比对，严重性策略门控
  - 定位3: 代码审计服务 — 专业团队深度分析+人工复核
  - 统一内核: 三种形态共享同一 Hive-Mind 审计引擎

## Page 8: Content - Hive-Mind架构总览
- **Page Type**: Content
- **Page Title**: Hive-Mind 架构：蜂巢心智协同审计
- **Content Structure**:
  - 步骤1: Pre-Scan 预扫描 — TreeSitter + OSV + DocIntel + Domain
  - 步骤2: Cerebrum 推理循环 — LLM主脑，最多30轮迭代
  - 步骤3: Drone 执行验证 — 8种角色专项执行器并发验证
  - 步骤4: Critic 质量审查 — ACCEPT/REJECT/NEEDS_REVIEW裁决
  - 步骤5: 报告输出 — SARIF/JSON/MD/DOT/GraphML

## Page 9: Content - Cerebrum四层认知模型
- **Page Type**: Content
- **Page Title**: Cerebrum：模拟安全研究员的认知过程
- **Content Structure**:
  - Layer 1: DEEP UNDERSTANDING — 构建系统心理模型，识别信任边界和数据流
  - Layer 2: ADVERSARIAL EMPATHY — 模拟开发者心态寻找盲点
  - Layer 3: ABDUCTIVE REASONING — 从异常出发而非已知漏洞模式
  - Layer 4: EPISTEMOLOGICAL HUMILITY — 主动设法打破自己的假设
  - 置信度校准: 0.90+ CERTAIN / 0.80+ HIGH / 0.60+ SUSPICIOUS / <0.60 不报告

## Page 10: Content - Drone执行器与Critic审查
- **Page Type**: Content
- **Page Title**: Drone 验证 + Critic 审查：双重质量保障
- **Content Structure**:
  - Drone 8种角色: evidence-collector / data-flow-tracer / state-validator / topology-mapper / exploit-crafter / harness-generator / crash-analyzer / scope-definer
  - Drone 特性: 沙箱隔离 · ReAct循环(50步) · 3轮chase · 6种工具
  - Critic 三问: REACHABLE / EXPLOITABLE / MITIGATED
  - 双重保障: Cerebrum→Drone→Critic→贝叶斯 四层过滤

## Page 11: Transition - 产品形态
- **Page Type**: Transition
- **Page Title**: 产品形态
- **Page Subtitle**: CLI · CI/CD · 审计服务

## Page 12: Content - CLI审计工具
- **Page Type**: Content
- **Page Title**: CLI 审计工具：一行命令启动深度扫描
- **Content Structure**:
  - 基本用法: zdll scan <target> — 本地路径或 Git URL
  - 配置灵活: workers/rounds/time 均可调
  - 恢复扫描: zdll resume — 中断后恢复状态
  - 报告格式: 5种格式输出
  - 技术优势: Go单二进制 · 跨平台 · 原子持久化

## Page 13: Content - CI/CD集成
- **Page Type**: Content
- **Page Title**: CI/CD 集成：DevSecOps 的安全扫描组件
- **Content Structure**:
  - CI 模式: zdll ci <target> — 增量扫描+基线比对
  - SARIF 输出: SARIF 2.1.0 工业标准
  - 基线比对: 新增发现 vs 已解决发现
  - 严重性门控: --fail-on 阻断部署
  - 增量扫描: --diff-base 只扫描变更文件

## Page 14: Transition - 技术亮点
- **Page Type**: Transition
- **Page Title**: 技术亮点
- **Page Subtitle**: 去重 · 置信度 · 领域知识

## Page 15: Content - 五层语义去重
- **Page Type**: Content
- **Page Title**: 五层语义去重：告别重复告警
- **Content Structure**:
  - Layer 0: Bloom预过滤 — O(1) 快速判断
  - Layer 1: 锚点匹配 — 函数名+文件名+unigram
  - Layer 2: 结构指纹 — SHA256哈希精确匹配
  - Layer 3: Bigram/Unigram Jaccard — 近似重复检测
  - Layer 4: 字符3-gram回退 — 终极兜底
  - 效果: 同义词标准化+停用词过滤+五层管道

## Page 16: Content - 贝叶斯置信度与领域知识
- **Page Type**: Content
- **Page Title**: 贝叶斯置信度 + 领域知识：精准与深度
- **Content Structure**:
  - 贝叶斯传播: confidence = 0.4×自身 + 0.6×父假设×因果强度
  - 强度学习: 历史结果持续学习因果强度矩阵
  - 领域 Terrain: 12个安全领域各有专属SKILL.md
  - 综合效果: 越用越准+领域专注+避免孤立评估

## Page 17: Transition - 对比优势
- **Page Type**: Transition
- **Page Title**: 对比优势
- **Page Subtitle**: 为什么不同

## Page 18: Content - vs传统工具对比
- **Page Type**: Content
- **Page Title**: 认知审计 vs 规则扫描：本质差异
- **Content Structure**:
  | 发现方式 | 规则匹配 | 溯因推理 |
  | 漏洞类型 | 已知模式 | 逻辑+零日 |
  | 误报控制 | 粗粒度过滤 | 五层+贝叶斯+Critic |
  | 领域适应 | 通用规则 | 12领域自动检测 |
  | CI集成 | 手动配置 | SARIF+基线+增量 |
  | 可解释性 | 规则ID | 推理过程+PoC |
  | 学习进化 | 人工更新 | 贝叶斯跨项目学习 |

## Page 19: Ending - 开始认知型代码安全审计
- **Page Type**: Ending
- **Page Title**: 开始认知型代码安全审计
- **Content Structure**:
  - 副标题: zerodayFind — 从假设出发，发现真实漏洞
  - CLI工具: zdll scan <your-project>
  - CI集成: 集成到 DevSecOps 流水线
  - 审计服务: 专业团队深度审计+人工复核
  - CTA: 联系我们获取试用

## Design Style
Tech — 现代科技风格，深蓝配色体系，棱角分明，适合安全产品定位。主色深蓝(#1E3A5F)，强调色亮蓝(#4A9EFF)，次要强调色绿色(#00C853)，中性色灰色(#6B7280)。标题字体 Montserrat，正文字体 Inter + Noto Sans SC。
