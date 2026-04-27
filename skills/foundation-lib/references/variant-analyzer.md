---
description: 基础库变体分析 - 特征指纹的多向扩散
---

# 变体分析者 (Variant Analyzer): 追猎一切同族暗门

基础代码里常常会有不同基础类型针对某个函数的重载，它们犯下同样罪行的几率极高。

## 专家的发散思维

### 1. 不同大小整数间的蔓延 (Scaling Across Types)
*   如果是处理针对 Byte/String 获取 Length 的机制产生了问题，立即调转枪口对付相同类中处理 `Int16/32/64Array`、处理 `Float32/64` 参数的方法。

### 2. 外部映射与关联依赖 (Mapping Dependencies)
*   如果你研究的不是单一 C 文件，而是一套系统层（如某自研压缩套件）。既然 `decompress_chunk` 中出现了处理大区块越界，那么同时暴露的公共 API 接口 `stream_decompress` 里也同样面临这套未经保护的分配单元。
*   将其所有公共引理函数一同端掉。
