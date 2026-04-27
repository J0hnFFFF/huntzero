---
description: AI Agent 代码理解 - 追踪数据投毒与指令执行的血脉
---

# 代码理解者 (Code Understander): 解构动态状态机

不要逐行看面条代码，要从大局上把控 AI Agent 这个“状态机”的流转逻辑。代码只是将 LLM 和工具缝合起来的胶水，你要看穿这层胶水。

## 专家的审计视角

### 1. 把控 Tool 的防御纵深（Defense in Depth）
大模型一定会被突破（Prompt Injection 是 inevitably successful 的）。专家从不寄希望于“系统提示词里写了不要执行危险命令”。
*   你需要直接去看 `tools/` 目录下的代码实现。
*   `read_file` 工具内部是否做了路径规范化和白名单检查？如果没有，这就是一个任意文件读取漏洞。
*   `run_command` 工具是否使用了 `shell=True`？参数是否做了转义？
*   **思想**：把包含工具的函数当作外部输入可直接控制的 C 语言函数，去寻找底层的资源滥用。

### 2. 分析 MCP（Model Context Protocol） / 插件架构配置
很多 Agent 允许动态加载 MCP Server。
*   MCP 的配置文件存放在哪里？配置修改的 API 是否有健全的鉴权？
*   如果攻击者能通过某种接口修改 MCP 配置，就能注册一个恶意工具。
*   甚至，工具的 `description` 字段如果来自不受信的地方，它本身也是一种针对 LLM 的 Prompt 注入载体。

### 3. 多智能体编排网络（The Agentic Graph）
如果是 LangGraph、CrewAI、AutoGen 架构。
*   理清节点（Node）间的跳转条件（Edges）。
*   哪些 Agent 承担“对外交互”的低权限角色，哪些 Agent 掌握着“操作服务器”的高权限角色？
*   它们之间的信息是如何传递的（State/Messages）？能否通过低权限 Agent 的输出来诱导高权限 Agent 偏离预期航线？

## 你的目标输出
详细描绘出代码中**缺乏底层拦截的危险工具函数**，以及多轮对话中**状态管理的漏洞点**。
