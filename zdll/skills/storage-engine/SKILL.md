---
description: 存储引擎领域安全第一性原理简报 (V7 Domain Intelligence Brief)
---

# 存储引擎地形情报 (Storage Engine Domain Brief)

> [!IMPORTANT]
> 在存储引擎（KV Store, LSM-Tree, 内存库如 Redis/TerarkDB）的世界，不要去寻找 SQL 注入。
> 这是系统最底层的“物流中心”。真正的黑客通过制造**数据压缩的谎言**与**并发时间差**，在这里引发毁灭整个母体应用的核爆。

## 第一性原理：黑客眼中的存储引击本质

存储引擎是榨干硬件每一点 I/O 及内存性能的极限系统。在它华丽的吞吐量指标下，潜伏着为了极速而放弃防护的机制：

### 原理一：前台与后台的时间裂隙 (The Foreground-Background Rift)
引擎内部总是多线程的：前台的 Iterator 在 `Seek()` 和 `Next()` 扫描数据，后台的 Compaction 线程在默默搬运和删除无用 Block。
*   **黑客视角**：这是 UAF 和野指针的温床。如果引用计数 (Refcount) 稍有疏漏，导致后台将一个前台仍在读取的底层 MemTable 或 SSTable 文件句柄关闭并释放，整个引擎将立刻面临 Core Dump。

### 原理二：竞技场分配器的溢出把戏 (Arena Allocation Overflow)
千万不要以为底层 libc 会保护引擎的内存。现代引擎全部自己写内存池（Arena Allocator）。
*   这些内存池分配新块时仅仅是一句 `ptr = current + size; current += size`。
*   如果黑客传入极大（或极小下溢）的 `size` 使 `current += size` 发生算数包裹（Wraparound），引擎毫无察觉地将接下来的新数据写入到与旧数据**物理重叠**的空间内。随后，通过控制这片幽灵区域的数据，黑客就能重写关键函数指针，一击取得 RCE。

### 原理三：压缩块的盲区读取 (The Blindness of Block Encoding)
如 TerarkDB/RocksDB 的底层 Block 存储了高度压缩的数据（共享前缀编码，Delta 压缩）。
*   为了极速解压，迭代器内常常写满裸循环，不带任何边界检查。
*   黑客通过在内存里刻入（或者替换磁盘上的数据文件）具有谎称有超长前缀的畸形变长整数（VarInt），诱使迭代器指针在解压时飞出分配边界，酿成严重的 OOB Read / Write 灾难。

### 原理四：垃圾回收陷阱与墓碑欺诈 (Tombstones & GC Exhaustion)
数据的物理删除总是滞后的。被删的数据留着被称为“墓碑”（Tombstone）的幽灵标记。
*   黑客或者通过创建超长寿命周期的 Snapshot 锁死 GC 导致长达几个月的 WAL 日志无法回收而直接撑爆磁盘（OOM/DoS）。
*   或者通过快照序列号 SeqNo 的比较漏洞，利用版本穿透技术读取本该属于高权限租户已经删除的内容。

### 原理五：重放恢复时的亡灵控制 (Log Replay Necromancy)
系统异常断电重启后，会走入往往缺乏严格校验的 `Recover()` 灾备逻辑中。
*   黑客利用并发截断一条写入过程中的 WAL 记录，然后主动发指令让引擎崩溃（或等待自然崩溃）。当系统再度拉起并重放这条毒记录时，因验证机制不到位而造成内存图与哈希链树折断崩溃。

### 原理六：自旋锁的脱轨博弈 (Spinlock / Lock-Free ABA slippage)
为追求微秒级延迟，存储引擎普遍采用了 Lock-free 或者自研 Spinlock。
*   在高并发写入和 Resize（哈希表扩容迁移）的交叉点，是产生 ABA 问题和条件竞争的黄金地段。黑客通过特定的交错发包节奏，卡在那个没上锁的瞬间将引擎拖垮。

---

## 你的行为准则
作为 KimiSec，在此领域：
1. 你的武器库里只有：超长 `VarInt`，边界极值，疯狂的 Iterator 占位符，以及 `OOM` 爆破。
2. 剥除上层的封装，直视 C++ 与 Rust 中那些标有 `unsafe` 或用着汇编算术的地方。在这等极致压缩的沙盘里，只需错位 1 个 byte 的指针偏移，你就能把整个引擎掀翻。
