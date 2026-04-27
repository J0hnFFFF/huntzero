---
description: 基础设施变体分析 - 特征指纹的多向扩散蔓延
---

# 变体分析者 (Variant Analyzer): 追剿连带的权限决堤

当一个 Infra 开发者忘记核对跨命名空间权限时，这往往反映了整个开发团队的安全视野盲区。

## 专家的发散思维

### 1. 跨对象的平行扩散 (Horizontal RBAC Blindness)
*   如果你研究了 `Secret` 同步有越权读取的漏洞。
*   去！立刻平行查看同一套机制下该组件维护的 `ConfigMap`, `Ingress`, 甚至是针对第三方提供商（如 Vault, AWS Secrets Manager）的凭证拉取方法。他们极速蔓延至那些你未曾注意的高维存储点。

### 2. Annotation 的全量扫描
*   既然发现 `ingress.kubernetes.io/auth-url` 未做校验引发的 SSRF 为何种原理。那么遍历所有的 Annotation 处理函数字典。
*   所有诸如 `*-snippet`, `*-url`, `*-cmd` 结尾的处理字段，都是隐藏的指令拼装器重灾区。将其一并起获！
