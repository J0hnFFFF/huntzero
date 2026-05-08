---
description: 测试存储引擎假设：迭代器压力测试、畸形 SSTable 注入、WAL 截断测试与并发读写竞争。
tags: [storage-engine, testing, fuzzing, iterator, race-condition]
---

# 假设测试 (Hypothesis Tester)

## 1. 专家直觉触发器 (Triggers)

专家在验证存储引擎安全假设时，会立即启动以下测试：

- **迭代器压力测试**：在高并发写入和 compaction 背景下，持续创建、遍历、销毁迭代器，观察是否出现崩溃或数据不一致。
- **畸形 SSTable 注入**：手动构造包含超大 VarInt、错误 checksum、越界索引指针的 SSTable，观察引擎在打开时是否崩溃。
- **WAL 截断与篡改**：在写入过程中 `kill -9` 引擎进程，或直接修改 WAL 文件的末尾几个字节，测试恢复逻辑的鲁棒性。
- **并发读写竞争**：使用 ThreadSanitizer (TSan) 编译引擎，运行多线程随机读写，检测 data race。
- **快照滥用测试**：创建大量快照且不释放，同时持续写入，观察磁盘使用是否无限增长并触发 OOM。
- **超大键值测试**：尝试写入接近或超过 `2^31` 字节的键或值，观察 allocator 和序列化逻辑的行为。

这些触发器覆盖了存储引擎最核心的质量属性：正确性、鲁棒性、性能和安全性。

## 2. 不可跳过的问题链 (Question Chain)

在设计测试前，必须回答：

1. **测试的输入空间如何覆盖边界？**  
   键长度 [0, 1, max-1, max, max+1]、值长度、批量大小，是否都包含？
2. **并发度是否足够触发 race？**  
   线程数是否大于 CPU 核心数？是否混合了读、写、删除、迭代、快照创建？
3. **畸形数据的构造是否贴近真实格式？**  
   SSTable 的 Footer、Index Block、Data Block、Filter Block 是否分别被篡改？
4. **测试的通过/失败判据是否客观？**  
   崩溃、ASAN/MSAN 报错、TSan data race、checksum mismatch，哪些算失败？
5. **测试环境是否与生产一致？**  
   是否使用了相同的编译器优化级别？是否启用了 jemalloc/tcmalloc？文件系统是否为 ext4/xfs？
6. **测试的可复现性如何保证？**  
   是否固定了随机种子？是否记录了完整的操作序列（如 crash test 的 log）？

## 3. 攻击链闭合 (Attack Chain Closure)

测试的核心是将“代码审计怀疑”转化为“确定性证据”。以下是测试 arena 溢出假设的完整逻辑：

**假设**：向存储引擎写入一个总大小恰好导致 `size_t` 溢出的键值对，会使 arena allocator 返回重叠指针。

**测试步骤 1（静态分析确认）**：在 `arena.cc` 中找到 `Allocate(size_t bytes)`，确认其使用 `ptr_ + bytes <= limit_` 或直接 `ptr_ += bytes`，且 `bytes` 来自用户输入的 `key.size() + value.size()`。
**测试步骤 2（构造触发输入）**：计算目标平台的 `SIZE_MAX`。若键和值长度均为 32 位无符号整数，构造 `key_size = 0x80000000`，`value_size = 0x80000000`，使总和在 32 位下回绕为 0，但在 64 位平台上不会。此时需寻找 64 位下的回绕点，如 `SIZE_MAX / 2 + X`。
**测试步骤 3（直接 API 测试）**：通过存储引擎的写入 API 发送构造的键值对。若引擎直接拒绝（返回 `InvalidArgument`），则上层有防护。
**测试步骤 4（绕过上层）**：若上层有校验，尝试通过 SSTable ingestion 直接注入一个手工构造的文件，绕过写入路径的长度检查。
**测试步骤 5（观察结果）**：使用 ASAN 编译引擎。若出现 `heap-buffer-overflow` 或 `stack-buffer-overflow`，则假设成立。
**测试步骤 6（评估可利用性）**：若溢出的内存覆盖了相邻的 `InternalKey` 结构，且该结构包含指向用户数据的指针，则可能被利用为信息泄露或控制流劫持。

**逻辑闭环**：如果 allocator 未对 `bytes` 做上限检查，且用户能通过任何路径传入超大尺寸，则溢出可被触发。因此，**ASAN 报错 + 绕过上层校验 = 漏洞确认**。

## 4. 代码示例 (Code Examples)

### 4.1 迭代器压力测试脚本

```python
#!/usr/bin/env python3
# stress_iterator.py
import subprocess
import threading
import os

DB_PATH = "/tmp/stress_db"

def writer():
    for i in range(100000):
        subprocess.run(["./db_bench", f"--db={DB_PATH}", f"--writes={i}"],
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

def reader():
    for i in range(100000):
        subprocess.run(["./db_bench", f"--db={DB_PATH}", "--reads=100"],
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

if __name__ == "__main__":
    os.system(f"rm -rf {DB_PATH}")
    t1 = threading.Thread(target=writer)
    t2 = threading.Thread(target=reader)
    t1.start(); t2.start()
    t1.join(); t2.join()
```

**说明**：通过并发读写触发 compaction 与迭代器的交叉，使用 ASAN/TSan 编译的引擎可快速发现 UAF 或 data race。

### 4.2 畸形 SSTable 注入工具

```cpp
// malformed_sstable.cc
#include <cstdint>
#include <fstream>

struct BlockHandle {
  uint64_t offset = 0;
  uint64_t size = 0xFFFFFFFFFFFFFFFF;  // 极大尺寸
};

int main() {
  std::ofstream f("bad.sst", std::ios::binary);
  // 写一个假的 SSTable Footer，size 字段为 -1
  BlockHandle meta_index, index;
  f.write(reinterpret_cast<const char*>(&meta_index), sizeof(meta_index));
  f.write(reinterpret_cast<const char*>(&index), sizeof(index));
  char magic[8] = {'\x57', '\xFB', '\x80', '\x8B', '\x24', '\x75', '\x47', '\xDB'};
  f.write(magic, 8);
  f.close();
  return 0;
}
```

**说明**：生成的 SSTable 拥有合法的 Magic Number 但非法的 BlockHandle。测试引擎在 `DB::Open` 时是否会因读取超大索引而崩溃。

### 4.3 WAL 截断测试脚本

```bash
#!/bin/bash
# wal_truncate_test.sh
DB_DIR="/tmp/wal_test"
rm -rf $DB_DIR

# 启动引擎写入后台进程
./db_write_load --db=$DB_DIR &
PID=$!
sleep 2

# 随机杀死进程模拟崩溃
kill -9 $PID

# 获取最新的 wal 文件并截断最后 20 字节
WAL=$(ls -t $DB_DIR/*.log | head -n 1)
truncate -s -20 $WAL

# 尝试恢复
./db_recover --db=$DB_DIR
echo "Recover exit code: $?"
```

**说明**：验证存储引擎在 WAL 末尾不完整时，是优雅地忽略最后一条记录，还是崩溃或回放垃圾数据。

### 4.4 并发读写 TSan 测试

```bash
#!/bin/bash
# tsan_test.sh
export CXX=clang++
export CXXFLAGS="-fsanitize=thread -fno-omit-frame-pointer"

make clean && make -j$(nproc)
./db_stress --ops_per_thread=10000 --threads=16 --reopen=10
```

**说明**：ThreadSanitizer 可检测 lock-free 结构、refcount 和 compaction 中的 data race。任何 TSan 报错都意味着潜在的崩溃或数据损坏。

### 4.5 快照 DoS 测试脚本

```python
#!/usr/bin/env python3
# snapshot_dos.py
import redis

r = redis.Redis()
# 创建 10000 个快照/保存点
for i in range(10000):
    r.execute_command("SAVEPOINT")
    # 持续写入
    r.set(f"key{i}", "x" * 1024)

print("Check disk usage: df -h")
```

**说明**：若存储引擎不限制快照数量或 WAL 保留长度，磁盘将在短时间内耗尽。此测试可验证 GC 策略的有效性。
