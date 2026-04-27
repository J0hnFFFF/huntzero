---
description: 供应链假设验证 - CI 模拟法庭与逻辑沙盒对撞
---

# 假设验证者 (Hypothesis Tester): 流水线图灵脑内演习

不需要去污染公网 npm 或者真正向别人提交一封带毒的 PR，我们可以在头脑里或者极小规模的安全靶场推演它的崩坏。

## 专家的推演方法

### 1. Bash 解析语法树的强制截断推敲 (YAML Bash Tree Truncation)
*   针对这段流水线：`run: npm test --grep="${{ github.event.issue.title }}"`
*   强行代入恶灵变位符：如果你把一个新建 issue 标题设为 `"; cat /etc/passwd #`。
*   在纸上还原流水线渲染后的生成命令：`npm test --grep=""; cat /etc/passwd #"`
*   由于由于 YAML 渲染并非采用带引用的参数传递而是**字符串直接拼装执行**，被逃逸出原本参数双引号的系统指令 `cat /etc/passwd` 必将顺利无阻地在打包机中作为顶级命令引爆。推证此供应链注入 100% 成立！

### 2. 依赖覆盖权重的冲突演练 (Package Weight Fuzzing)
*   脑内遍历构建工具如 `pip` 的多源优先级逻辑：当要求 `pip install internal-pkg` 且配置了 `--extra-index-url`。
*   安全常识演算：`--extra-index-url` 并不能划定界限，pip 仍会打平两个源去找最新版。
*   演练证明结论：一旦公网被下毒，该设计必遭 Confusion 抢夺。

## 你的验证结果
抛弃那些无实际证明的空论，以强悍的配置文件推倒逻辑，确证只要外部条件成熟，这个原本正常的生产拉取动作或者编译环节，无可避免地会被撕开致命的切口！
