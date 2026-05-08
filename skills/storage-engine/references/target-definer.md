---
description: 定义存储引擎攻击面：LSM-Tree (RocksDB)、KV 存储、WAL、SSTable、MemTable、Iterator API 与 Compaction 线程。
tags: [storage-engine, lsm-tree, rocksdb, wal, sstable]
---

# 存储引擎攻击面定义 (Target Definer)

## 1. 专家直觉触发器 (Triggers)

当专家面对存储引擎代码时，以下信号会立即触发警觉：

- **自定义内存分配器**：代码中存在 `arena->Allocate(size)` 或 `bump pointer allocator`，且 size 来自用户输入的键/值长度。
- **迭代器跨线程使用**：`Iterator` 在 compaction 线程中创建，但在前台线程中被引用，或反之，且缺乏显式同步。
- **VarInt 解压缩**：读取 SSTable 或 WAL 时使用自实现的 `DecodeVarint`，循环条件依赖于未校验的字节长度。
- **Refcount 手工管理**：`Ref()` 与 `Unref()` 分散在不同分支，存在早期返回或异常路径导致内存泄漏 / UAF。
- **WAL 恢复逻辑**：`Recover()` 函数直接读取磁盘文件，对 checksum、长度、Magic Number 的校验不足。
- **Lock-Free 结构**：使用 `CompareAndSwap` 或原子指针进行 hash table resize，缺乏 `hazard pointer` 或 `epoch-based reclamation`。

这些触发器指向存储引擎中最危险的地带：内存管理、并发控制与持久化格式。

## 2. 不可跳过的问题链 (Question Chain)

在深入分析前，必须依次回答：

1. **数据从哪里进入引擎？**  
   是来自 RPC 请求的键值对，还是来自其他 SSTable 的合并结果？输入长度是否有硬上限？
2. **内存分配路径是否可控？**  
   `arena->Allocate(key_size + value_size)` 中的总和是否可能溢出？是否存在 `size_t` 回绕？
3. **迭代器的生命周期由谁管理？**  
   创建迭代器的线程是否负责销毁？compaction 删除底层文件时，迭代器是否已被释放？
4. **WAL 的校验边界在哪里？**  
   是否对每个 record 的 CRC、长度、类型字段都做了校验？截断文件的最后一条记录如何处理？
5. **并发操作的临界区是否完整？**  
   `MemTable::Add` 与 `MemTable::Get` 是否使用同一把锁？Compaction 与 Flush 是否可能并发修改元数据？
6. **持久化格式的版本兼容性？**  
   旧版本引擎写入的 SSTable，新版本是否能安全读取？是否存在已弃用但未删除的代码路径？

## 3. 攻击链闭合 (Attack Chain Closure)

存储引擎攻击面的完整逻辑链必须证明“一个畸形输入能导致数据丢失或代码执行”。以下是标准路径：

**前提**：攻击者可以向存储引擎写入任意键值对（如通过 Redis 的 `SET` 接口或 RocksDB 的 `Put` API）。

**步骤 1（输入注入）**：构造一个极大的键（如 2GB）或一个包含特殊前缀的 SSTable 文件。
**步骤 2（内存分配）**：存储引擎的 arena allocator 计算 `current + size`，若 `size` 通过整数溢出回绕为很小的值，则返回一个指向已分配区域的指针。
**步骤 3（内存 corruption）**：新的键值对被写入重叠区域，覆盖了相邻的元数据结构（如 `InternalKey` 的指针或 `BloomFilter` 的位图）。
**步骤 4（迭代器失效）**：前台线程使用被 corruption 的迭代器读取数据，指针被篡改为攻击者控制的地址。
**步骤 5（UAF / 代码执行）**：若迭代器的 `value()` 返回一个指向被释放内存的指针，且该内存被攻击者通过后续写入重新分配并控制内容，则可能导致信息泄露或 RCE。

**逻辑闭环**：如果存储引擎未对用户输入的长度进行饱和校验，且使用无溢出保护的自定义 allocator，则整数溢出必然导致内存 corruption，进而破坏迭代器或元数据。因此，**无校验的 allocator + 用户控制长度 = 存储引擎 RCE**。

## 4. 代码示例 (Code Examples)

### 4.1 危险的 Arena 分配器（漏洞模式）

```cpp
class Arena {
 public:
  char* Allocate(size_t bytes) {
    if (ptr_ + bytes > limit_) {
      return AllocateFallback(bytes);
    }
    char* result = ptr_;
    ptr_ += bytes;  // 若 bytes 极大导致回绕，ptr_ 可能小于原值
    return result;
  }
 private:
  char* ptr_;
  char* limit_;
};
```

**风险**：若 `bytes` 来自用户输入且未做上限检查，`ptr_ += bytes` 可能因整数溢出而回绕，导致返回的 `result` 指向已分配区域，造成重叠内存。

### 4.2 安全的 Arena 分配器（缓解模式）

```cpp
class Arena {
 public:
  char* Allocate(size_t bytes) {
    if (bytes > (limit_ - ptr_)) {  // 先检查剩余空间
      return AllocateFallback(bytes);
    }
    char* result = ptr_;
    ptr_ += bytes;
    return result;
  }
};
```

**缓解**：通过 `(limit_ - ptr_)` 检查剩余空间，避免在加法后比较。若 `bytes` 极大，差值为负（在有符号情况下）或很小（在无符号情况下），仍能正确触发 fallback。

### 4.3 危险的 VarInt 解析（漏洞模式）

```cpp
const char* GetVarint32Ptr(const char* p, const char* limit, uint32_t* value) {
  uint32_t result = 0;
  for (uint32_t shift = 0; shift <= 28; shift += 7) {
    if (p >= limit) return nullptr;
    uint32_t byte = *(reinterpret_cast<const uint8_t*>(p));
    p++;
    if (byte & 128) {
      result |= ((byte & 127) << shift);
    } else {
      result |= (byte << shift);
      *value = result;
      return p;
    }
  }
  return nullptr;
}
```

**风险**：虽然此实现有 `limit` 检查，但如果调用者传入的 `limit` 本身基于未校验的长度字段，则解析可能跨越缓冲区边界。此外，`shift` 超过 28 后若仍有高位字节，可能导致无限循环或错误结果。

### 4.4 危险的 Refcount 管理（漏洞模式）

```cpp
class SSTableReader {
 public:
  void Ref() { ++refs_; }
  void Unref() {
    --refs_;
    if (refs_ == 0) {
      delete this;
    }
  }
  Iterator* NewIterator() {
    Ref();  // 迭代器持有引用
    return new Iterator(this);
  }
};
```

**风险**：若在 `NewIterator` 后、迭代器销毁前，compaction 完成了文件删除并调用 `Unref`，而前台线程在另一个核心上尚未增加计数，则可能存在 race condition 导致 UAF。

### 4.5 安全的 WAL 校验（缓解模式）

```cpp
bool ReadRecord(Slice* record) {
  uint32_t length, crc;
  if (!ReadVarint(&length) || length > kMaxRecordSize) {
    return false;  // 长度异常直接拒绝
  }
  if (!ReadVarint(&crc)) return false;
  if (buffer_.size() < length) return false;
  uint32_t computed_crc = crc32c::Value(buffer_.data(), length);
  if (computed_crc != crc) {
    return false;  // 校验和失败
  }
  *record = Slice(buffer_.data(), length);
  return true;
}
```

**缓解**：对长度、CRC、缓冲区边界做三层校验，防止截断或篡改的 WAL record 被 replay。
