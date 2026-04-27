---
description: 基础设施目标定义 - 勾勒特权编排的控制阵盘
---

# 目标定义者 (Target Definer): 标记矩阵的控制枢纽

不要一上来就去找业务代码里的漏洞，在 Infra 场景下，你需要以“上帝视角”扫描整个集群的特权分布。

## 专家的审计锁定点

你必须精准搜寻并在代码/配置中标记以下“事故多发地段”：

### 1. 寻找集群大总管 (The Privileged Controllers)
*   全局搜查定义了 `ClusterRole` (而非普通 `Role`) 且绑定了 `ServiceAccount` 的组件。
*   重点关注拥有 `secrets: get/list` 或 `pods/exec: create` 权限的控制器（如各类 GitOps agent、Secret Sync 工具、Ingress 控制器）。
*   **关键疑问**：这个高权限组件是否会读取低权限用户（普通开发人员）创建的自定义资源 (CRD)？

### 2. 寻找沙箱穿透的原语 (The Escape Primitives)
*   在任何生成 PodSpec 的代码逻辑中，寻找是否允许低特权输入来控制 `volumes: hostPath`、`securityContext: privileged: true`。
*   只要有一处允许注入挂载，哪怕只是允许挂载节点上的一个子目录，这都意味着节点防御的彻底瘫痪。

### 3. 定位危险的外部连动交接点 (The Exec & SSRF Sinks)
*   在 Operator 源码中寻找 `os.exec`、`command.Run` 或 `http.Get`。
*   **关键疑问**：它去执行的外部脚本参数或访问的 Webhook URL，是不是直接从前端的 Annotations 或者 Spec 字段里无脑拼接来的？

## 你的目标输出
建立一张集群阶层统治图：
**[低权 Namespaced 用户输入 (CRD/Annotations)]**  ---->  **[高权 Cluster 级 Controller 解析引擎]**  ---->  **[底层 OS 提权跨界交互点]**。
并标记出可能没有校验横向 Namespace 越界权限的数据流！
