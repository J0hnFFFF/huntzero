---
description: 在存储引擎中狩猎漏洞：迭代器引用计数、Arena 溢出、VarInt 解析、快照滥用、WAL 损坏与 resize 竞争条件。
tags: [storage-engine, vulnerability-hunting, rocksdb, iterator, race-condition]
---

# 存储引擎漏洞狩猎 (Vuln Hunter)

## 1. 专家直觉触发器 (Triggers)

专家在审计存储引擎时，会立即对以下代码模式启动深度狩猎：

- **迭代器未追踪底层文件生命周期**：`Iterator` 被返回给调用者，但对应的 SSTable 可能在后台 compaction 中被删除。
- **Arena 分配未检查上限**：`Allocate(n)` 中的 `n` 是两个 32 位整数之和，可能回绕。
- **VarInt 解码循环缺乏退出条件**：while 循环依赖字节的高位，但未限制最大读取字节数。
- **快照长期持有**：用户创建 `Snapshot` 后未释放，导致 WAL 和旧 SSTable 无法 GC，最终磁盘耗尽。
- **WAL 截断后无处理**：文件系统崩溃后 WAL 末尾不完整，`Recover()` 未正确忽略或报错。
- **Resize 时无锁遍历**：hash table 扩容期间，读线程可能遍历到正在迁移的 slot，读到悬空指针。

这些触发器覆盖了存储引擎的五大核心危险区：内存管理、并发、持久化、迭代器与资源管理。

## 2. 不可跳过的问题链 (Question Chain)

在狩猎前，必须回答：

1. **哪些输入字段直接影响内存分配大小？**  
   键长度、值长度、批量请求中的条目数，是否经过 `<= kMaxXXX` 检查？
2. **迭代器的引用是由谁负责？**  
   创建迭代器的代码是否调用了 `Ref()`？销毁时是否调用了 `Unref()`？异常路径是否漏掉？
3. **VarInt / 前缀长度字段的最大合法值是多少？**  
   若 SSTable 中某个字段声称长度为 `0xFFFFFFFF`，解析器是否会尝试分配 4GB 内存？
4. **快照的释放是否有超时或强制机制？**  
   如果客户端崩溃未释放快照，存储引擎是否有 Lease 过期或守护线程清理？
5. **WAL 的每条记录是否有独立 checksum？**  
   单条 record 损坏时，是停止恢复、跳过，还是尝试继续？跳过是否会导致状态不一致？
6. **Lock-Free 结构使用了哪种内存回收机制？**  
   是 `hazard pointer`、`epoch-based`，还是完全无回收？无回收意味着 ABA 和 UAF 随时发生。

## 3. 攻击链闭合 (Attack Chain Closure)

漏洞狩猎必须建立从“代码模式”到“可利用后果”的因果链。以下是迭代器 UAF 的完整狩猎逻辑：

**场景**：某 KV 存储允许用户创建快照并获取迭代器。Compaction 线程在后台合并 SSTable 并删除旧文件。

**狩猎步骤 1（定位生命周期）**：搜索 `NewIterator` 实现，发现其调用了 `table->Ref()`，但仅在迭代器 `Close()` 时调用 `Unref()`。
**狩猎步骤 2（寻找 Race Window）**：Compaction 线程在 `CompactRange()` 完成后立即调用 `RemoveObsoleteFiles()`，后者遍历所有 SSTable 并删除 `refs_ == 0` 的文件。
**狩猎步骤 3（构造竞争）**：前台线程创建迭代器后，恰好发生上下文切换。Compaction 完成，文件被删除。前台线程恢复执行，调用迭代器的 `Seek()`，访问已删除 SSTable 的索引块内存。
**狩猎步骤 4（验证崩溃）**：通过高并发压力测试（`db_bench` 或定制 client）同时创建迭代器和触发 compaction，观察是否出现 `SIGSEGV` 或 ASAN `heap-use-after-free`。
**狩猎步骤 5（武器化评估）**：若 UAF 发生在 `Iterator::value()` 返回的 `Slice` 中，且该 `Slice` 指向 mmap 的区域，则可能被利用为信息泄露。若指向 arena 分配的内部节点，则可能通过后续写入控制内容，实现代码执行。

**逻辑闭环**：如果迭代器的生命周期与底层文件的生命周期由不同线程管理，且缺乏同步机制（如 epoch 或文件句柄引用），则 UAF 必然存在。因此，**跨线程迭代器 + 无同步 = UAF**。

## 4. 代码示例 (Code Examples)

### 4.1 危险的迭代器生命周期（漏洞模式）

```cpp
Iterator* DBImpl::NewIterator(const ReadOptions& options) {
  Iterator* internal_iter = table_cache_->NewIterator(
      options, files_);
  // 危险：internal_iter 被包装后返回，但未增加底层 SSTable 的引用计数
  return NewDBIterator(internal_iter, options.snapshot);
}
```

**风险**：若 `table_cache_` 未在 `NewIterator` 时增加 SSTable 的 `refs_`，compaction 删除文件后迭代器失效。

### 4.2 安全的迭代器生命周期（缓解模式）

```cpp
Iterator* DBImpl::NewIterator(const ReadOptions& options) {
  SequenceNumber snapshot = versions_->LastSequence();
  std::vector<FileMetaData*> files;
  versions_->current()->GetOverlappingInputs(0, nullptr, nullptr, &files);
  for (auto* f : files) {
    f->refs++;  // 显式增加引用
  }
  Iterator* iter = table_cache_->NewIterator(options, files);
  return NewDBIterator(iter, snapshot, [files]() {
    for (auto* f : files) {
      f->refs--;  // 析构时释放
    }
  });
}
```

**缓解**：在创建迭代器时增加所有相关文件的引用，并在迭代器销毁时通过 lambda 或析构函数释放。

### 4.3 危险的 VarInt 分配（漏洞模式）

```cpp
bool GetLengthPrefixedSlice(const char* data, size_t n, Slice* result) {
  uint32_t len;
  const char* p = GetVarint32Ptr(data, data + n, &len);
  if (p == nullptr) return false;
  *result = Slice(p, len);  // len 可能极大，导致后续越界读取
  return true;
}
```

**风险**：`len` 来自用户数据且未经上限校验。若 `p + len > data + n`，则 `Slice` 构造了一个越界视图，后续读取将访问无效内存。

### 4.4 安全的 VarInt 处理（缓解模式）

```cpp
bool GetLengthPrefixedSlice(const char* data, size_t n, Slice* result) {
  uint32_t len;
  const char* p = GetVarint32Ptr(data, data + n, &len);
  if (p == nullptr || len > (data + n - p) || len > kMaxRecordSize) {
    return false;
  }
  *result = Slice(p, len);
  return true;
}
```

**缓解**：检查 `len` 不超过剩余缓冲区大小，且不超过全局上限 `kMaxRecordSize`。

### 4.5 危险的快照管理（漏洞模式）

```cpp
class SnapshotList {
 public:
  SnapshotImpl* New(SequenceNumber seq) {
    SnapshotImpl* s = new SnapshotImpl(seq);
    snapshots_.push_back(s);
    return s;
  }
  // 无自动释放机制
};
```

**风险**：若客户端创建快照后崩溃或未调用 `Release()`，快照永远存在于链表中，阻止 WAL 和旧 SSTable 的清理，导致磁盘耗尽 DoS。

### 4.6 安全的快照管理（缓解模式）

```cpp
class SnapshotList {
 public:
  SnapshotImpl* New(SequenceNumber seq) {
    std::lock_guard<std::mutex> lock(mu_);
    SnapshotImpl* s = new SnapshotImpl(seq, this);
    snapshots_.push_back(s);
    return s;
  }
  void Remove(SnapshotImpl* s) {
    std::lock_guard<std::mutex> lock(mu_);
    snapshots_.remove(s);
    delete s;
  }
  void CleanupExpired() {
    std::lock_guard<std::mutex> lock(mu_);
    auto now = std::chrono::steady_clock::now();
    for (auto it = snapshots_.begin(); it != snapshots_.end(); ) {
      if ((*it)->Expired(now)) {
        delete *it;
        it = snapshots_.erase(it);
      } else {
        ++it;
      }
    }
  }
};
```

**缓解**：引入超时机制与守护线程定期清理过期快照，防止资源泄漏。
