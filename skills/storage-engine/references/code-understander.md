---
description: 阅读存储引擎代码：Arena 分配器、迭代器生命周期、Compaction 逻辑、WAL 格式与引用计数管理。
tags: [storage-engine, code-review, allocator, iterator, compaction, wal]
---

# 存储引擎代码理解 (Code Understander)

## 1. 专家直觉触发器 (Triggers)

专家阅读存储引擎源码时，会立即聚焦于以下模块：

- **Arena / BlockAllocator 实现**：分配、对齐、回退逻辑是否处理了超大分配和溢出。
- **Iterator 继承体系**：`InternalIterator`、`TwoLevelIterator`、`MergingIterator` 的 `Seek`、`Next`、`Prev` 是否可能在无效状态下调用。
- **Compaction 状态机**：`PickCompaction`、`DoCompactionWork`、`InstallCompactionResults` 之间是否原子地更新版本信息。
- **WAL 编码格式**：每条 Record 的 Header（checksum、length、type）与 Body 的边界是否严格对齐。
- **VersionSet / Manifest**：元数据文件的读写是否使用了与数据文件相同的校验机制。
- **Refcount / Deleter 机制**：`Cache` 中的 `deleter` 函数指针是否可能在对象被释放后调用。

这些触发器帮助专家快速建立代码的心智模型，并定位最危险的交互边界。

## 2. 不可跳过的问题链 (Question Chain)

在阅读代码时，必须追问：

1. **内存分配失败如何处理？**  
   `AllocateFallback` 调用 `new char[size]` 失败时，是返回 `nullptr` 还是抛出异常？上层是否检查了返回值？
2. **迭代器的 `valid()` 状态与调用约定？**  
   在 `Seek` 失败后调用 `Next()` 是否会导致未定义行为？`MergingIterator` 在子迭代器无效时如何比较键？
3. **Compaction 期间前台写入去哪里？**  
   新的 `MemTable` 何时切换？旧 `MemTable` 刷盘期间的新写入是否进入新的 `MemTable`？
4. **WAL 的 record type 有哪些？**  
   `kFullType`、`kFirstType`、`kMiddleType`、`kLastType` 的拼接逻辑是否能抵御中间人篡改？
5. **引用计数为 0 时的销毁路径？**  
   `Unref() -> delete this` 时，是否持有了某些锁？是否在回调中触发了新的 `Ref/Unref` 导致重入？
6. **跨平台兼容性风险？**  
   文件锁 `fcntl` vs `LockFile`、大小端序、`mmap` 行为在不同操作系统上是否一致？

## 3. 攻击链闭合 (Attack Chain Closure)

理解存储引擎代码的目标是证明“一个错误的状态转换会导致不可恢复的数据损坏”。以下是 compaction 与版本安装的逻辑链：

**代码场景**：Compaction 完成生成了新的 SSTable 文件，需要将其加入当前 `Version` 并删除旧文件。

```cpp
void DBImpl::InstallCompactionResults(CompactionState* compact) {
  mutex_.AssertHeld();
  compact->builder->Finish();
  compact->outputs.push_back(
      FileMetaData(file_number, file_size, smallest, largest));
  versions_->LogAndApply(compact->edit);
  // 旧文件在下一次 background 循环中删除
}
```

**步骤 1（元数据更新）**：`compact->edit` 记录了删除哪些旧文件、添加哪些新文件。
**步骤 2（日志持久化）**：`LogAndApply` 将 edit 写入 Manifest 文件。若此时进程崩溃，重启后需从 Manifest 恢复。
**步骤 3（版本切换）**：新的 `Version` 生效后，前台读取操作应只看到新文件。
**步骤 4（文件删除）**：旧文件的 `refs_` 降为 0，后台线程删除物理文件。
**风险点**：若 `LogAndApply` 成功，但 `compact->edit` 中错误地标记了仍在被迭代器使用的文件为删除，则迭代器将访问已删除的 SSTable。若 Manifest 写入失败但内存版本已切换，则重启后出现数据丢失。

**逻辑闭环**：如果 compaction 的元数据更新与文件生命周期管理不是原子操作，且缺乏引用计数保护，则必然存在数据丢失或迭代器崩溃路径。因此，**非原子版本切换 + 文件删除 = 数据损坏**。

## 4. 代码示例 (Code Examples)

### 4.1 危险的 Arena 对齐分配（漏洞模式）

```cpp
char* Arena::AllocateAligned(size_t bytes) {
  const int align = sizeof(void*);
  size_t current = reinterpret_cast<uintptr_t>(ptr_);
  size_t slop = (align - current) & (align - 1);
  size_t needed = bytes + slop;
  if (needed > static_cast<size_t>(limit_ - ptr_)) {
    return AllocateFallback(bytes);
  }
  char* result = ptr_ + slop;
  ptr_ += needed;
  return result;
}
```

**风险**：若 `bytes` 极大，`needed` 可能回绕。`slop` 计算本身安全，但 `bytes + slop` 可能溢出，导致 `needed` 变小，通过检查后写入越界。

### 4.2 安全的 Arena 对齐分配（缓解模式）

```cpp
char* Arena::AllocateAligned(size_t bytes) {
  const int align = sizeof(void*);
  size_t current = reinterpret_cast<uintptr_t>(ptr_);
  size_t slop = (align - current) & (align - 1);
  if (bytes > static_cast<size_t>(limit_ - ptr_) - slop) {
    return AllocateFallback(bytes);
  }
  char* result = ptr_ + slop;
  ptr_ += (bytes + slop);
  return result;
}
```

**缓解**：将加法转换为减法，先检查 `limit_ - ptr_ - slop >= bytes`，避免 `bytes + slop` 溢出。

### 4.3 危险的 Compaction 文件安装（漏洞模式）

```cpp
Status DBImpl::FinishCompaction(Compaction* c) {
  Status s = c->BuildOutputs();
  if (!s.ok()) return s;
  // 危险：未持锁即更新版本
  versions_->current()->AddFile(c->outputs());
  DeleteObsoleteFiles();  // 可能删除仍被读取的文件
  return Status::OK();
}
```

**风险**：`AddFile` 与 `DeleteObsoleteFiles` 之间缺乏互斥，前台迭代器可能正在读取即将被删除的旧文件。

### 4.4 安全的 Compaction 文件安装（缓解模式）

```cpp
Status DBImpl::FinishCompaction(Compaction* c) {
  Status s = c->BuildOutputs();
  if (!s.ok()) return s;
  VersionEdit edit;
  edit.AddFiles(c->outputs());
  edit.DeleteFiles(c->inputs());
  {
    std::lock_guard<std::mutex> l(mutex_);
    s = versions_->LogAndApply(&edit);
  }
  if (s.ok()) {
    DeleteObsoleteFiles();  // 在版本提交后，由后台安全删除
  }
  return s;
}
```

**缓解**：所有版本元数据更新在持有锁的情况下进行，并通过 `LogAndApply` 保证 Manifest 持久化与内存版本一致。

### 4.5 危险的 WAL 恢复（漏洞模式）

```cpp
Status DBImpl::RecoverLogFile(uint64_t log_number) {
  log::Reader reader(file);
  Slice record;
  while (reader.ReadRecord(&record)) {
    WriteBatch batch;
    batch.SetContents(record);  // 直接信任 record 内容
    batch.InsertInto(mem_);
  }
  return Status::OK();
}
```

**风险**：`ReadRecord` 可能返回截断或损坏的记录，`SetContents` 未校验内部键值对的合法性，可能将畸形数据插入 MemTable。

### 4.6 安全的 WAL 恢复（缓解模式）

```cpp
Status DBImpl::RecoverLogFile(uint64_t log_number) {
  log::Reader reader(file, true);  // checksum = true
  Slice record;
  while (reader.ReadRecord(&record)) {
    WriteBatch batch;
    Status s = batch.SetContents(record);
    if (!s.ok()) {
      return Status::Corruption("bad write batch", s.ToString());
    }
    s = batch.InsertInto(mem_);
    if (!s.ok()) return s;
  }
  return Status::OK();
}
```

**缓解**：启用 WAL reader 的 checksum 校验，并在 `SetContents` 和 `InsertInto` 的每个阶段检查错误状态。
