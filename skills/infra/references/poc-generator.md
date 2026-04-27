---
description: 基础设施 PoC 生成 - 一击必现的逃逸绞刑架
---

# PoC 生成器 (PoC Generator): 锻造无可辩驳的逃逸实证

让云管理团队不得不面对他们亲手搭建的集群已不再安全的残酷事实，必须提供直击灵魂的实操作业步序列。

## 专家级 PoC 的要求

1.  **脱水的命令行复现实锤**：
    提供能够即时粘贴跑在一台装有相应组件测试机器上的 Bash 指令流程：
    *   **步骤一**：先使用 `kubectl create namespace testuser` 并在极低权限沙箱登入，宣示我们处于没有任何能力的绝境。
    *   **步骤二**：然后直接用 `kubectl apply -f exp.yaml` 投递我们编纂好的邪恶特权窃取声明（由上一阶段生成）。
    *   **步骤三**：数秒内等待 Controller 执行了它错误的判决。并紧接着使用 `kubectl get secret -n testuser stolen-goods -o jsonpath='{.data.*}' | base64 -d`。
    *   **结果暴击**：成功在终端打印出了它绝不该看到的最高全域管理 `admin-token` 或者其它核心组件如 ETCD 的根秘钥！

2.  **CLAIM CHAIN 中的致命链判定**：
    宣誓必须如解构矩阵般精确到具体漏洞诱发源码块，清楚标明：`[file: controller.go:134 -> Use of Global SA context instead of User Impersonation check]`。

## 你的任务
拿出一套简洁连贯的 Kubectl 执行脚本集。用毫无修饰的操作，将云原生自傲的安全壁垒摧毁殆尽以昭示天下。
