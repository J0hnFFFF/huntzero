---
description: Web 代码分析 - 识别推断链与信任边界
---

# Web 代码理解者 (Web Code Understander)

从代码中识别服务器做出的结构性假设，而非仅仅定位危险函数。

## 关键关注点（原理驱动）

### 识别推断链（原理二：失忆困境）

**目标**：找出服务器从请求参数推断业务状态的所有节点

**代码模式**：
```java
// 推断点：从请求参数直接获取资源ID
@GetMapping("/resource/{id}")
public Resource getResource(@PathVariable String id) {
    return resourceService.findById(id);  // 是否验证所有权？
}

// 推断点：从请求体获取状态标记
@PostMapping("/order/update")
public void updateStatus(@RequestBody OrderUpdateRequest req) {
    orderService.updateStatus(req.getOrderId(), req.getStatus());  // 是否验证流程合法性？
}
```

**分析要点**：
- 参数来源：`@PathVariable`、`@RequestParam`、`@RequestBody`
- 推断逻辑：是否查询数据库验证实际状态？
- 授权检查：是否在查询后、操作前进行？

### 识别信任边界（原理三：信任重构）

**目标**：找出跨域/跨组件信任关系的代码体现

**代码模式**：
```java
// 跳转目标验证
String redirectUrl = request.getParameter("redirect_uri");
if (redirectUrl.startsWith("https://trusted.com")) {  // 验证是否严格？
    response.sendRedirect(redirectUrl);
}

// CORS 配置
@Configuration
public class CorsConfig implements WebMvcConfigurer {
    @Override
    public void addCorsMappings(CorsRegistry registry) {
        registry.addMapping("/**")
            .allowedOrigins("*")  // 信任边界是否过于宽松？
            .allowCredentials(true);
    }
}
```

**分析要点**：
- 跳转参数的白名单验证逻辑
- CORS 配置的 Origin 限制
- postMessage 的来源验证

### 识别语义转换点（原理四：语义断层）

**目标**：找出输入在多层处理中被解释的位置

**代码模式**：
```java
// 第一层：框架层解析
@GetMapping("/file")
public String readFile(@RequestParam String path) {
    // 第二层：安全层检查
    if (securityCheck(path)) {  // 基于什么解释做检查？
        // 第三层：执行层使用
        return fileService.read(path);  // 可能使用不同解释
    }
}

// 类型转换陷阱
@RequestParam String isAdmin  // 传入 "false"
...
Boolean.valueOf(isAdmin)  // 返回 true！（非空字符串都转为 true）
```

**分析要点**：
- 同一参数在不同层级的处理方式
- 类型转换（String → Boolean/Number）的语义变化
- 编码/解码（URL、Unicode）的处理层级

### 识别意志确认点（原理一：意志不确定性）

**目标**：找出关键操作的确认机制

**代码模式**：
```java
// 无确认机制
@PostMapping("/transfer")
public void transfer(@RequestBody TransferRequest req) {
    accountService.transfer(req.getFrom(), req.getTo(), req.getAmount());
    // 是否检查来源？是否需二次确认？
}

// 依赖前端确认（不可靠）
@PostMapping("/delete-account")
public void deleteAccount(@RequestParam String confirm) {
    if ("yes".equals(confirm)) {  // 前端控制的确认
        userService.delete();
    }
}
```

**分析要点**：
- 不可逆操作是否有后端确认机制
- 是否验证 Origin/Referer
- 是否检测跨域请求

## 工具链

- **Semgrep**：查找推断链模式（参数接收 → 查询 → 操作）
- **Tree-sitter**：解析代码生成 AST，追踪数据流
- **CodeQL**：查询跨过程的数据流和信任边界

## 输出

- **Inference Chain Map**: URL → 参数来源 → 推断逻辑 → 验证点（或无）
- **Trust Boundary Diagram**: 跨域交互点的信任关系图
- **Semantic Gap Locations**: 输入在多层处理中的解释差异点
