#!/usr/bin/env python3
"""
精准修复 engine/drone.py 中的资源泄漏问题。

策略：
1. 读取文件为字符串
2. 使用精确的字符串替换（避免正则和空白字符问题）
3. 添加 _acquire_session() 上下文管理器
4. 修改 execute() 使用 async with
"""

import re

def fix_drone_resource_leak():
    file_path = "engine/drone.py"
    
    with open(file_path, 'r', encoding='utf-8') as f:
        content = f.read()
    
    original = content
    modified = False
    
    # ─────────────────────────────────────────────────────────────────────
    # 步骤 1：在 execute() 之前插入 _acquire_session() 方法
    # ─────────────────────────────────────────────────────────────────────
    
    # 查找 "执行入口" 注释和 execute() 方法
    # 使用字符串查找（不是正则）
    marker = "    # ── 执行入口"
    
    idx = content.find(marker)
    if idx == -1:
        print("❌ Cannot find '执行入口' marker")
        return False
    
    # 向后查找 execute() 方法定义
    execute_def = "\n    async def execute(self) -> str:"
    idx2 = content.find(execute_def, idx)
    
    if idx2 == -1:
        print("❌ Cannot find 'async def execute()'")
        return False
    
    print(f"✅ Found insertion point at position {idx2}")
    
    # 构造 _acquire_session() 方法（使用制表符缩进）
    # 注意：文件使用制表符（\t）缩进
    new_method = '''    # ── Session 获取上下文管理器 ────────────────────────────────────────────────────

    @asynccontextmanager
    async def _acquire_session(self):
        """
        统一的 Session 获取上下文管理器。

        Security & Correctness:
        - 使用 async with 确保 Session 正确释放
        - 无论是否从池中获取，都能正确清理
        - 异常情况下也能保证资源释放
        """
        pool = self._session_pool or getattr(Drone, "_session_pool", None)
        session = None
        from_pool = False
        pool_cm = None

        try:
            if pool is not None:
                # 尝试从池中获取
                pool_cm = pool.acquire(self.drone_role)
                try:
                    async with pool_cm as pooled_session:
                        if pooled_session is not None:
                            session = pooled_session
                            from_pool = True
                            # 确保目标链接存在
                            slot = getattr(pool_cm, "_slot", None)
                            sandbox_dir = getattr(slot, "sandbox_dir", None)
                            if sandbox_dir is not None:
                                self._ensure_target_link(Path(sandbox_dir))
                            yield session, from_pool
                            return  # 正常退出 async with
                except Exception:
                    # 池获取失败，降级到按需创建
                    if pool_cm is not None:
                        try:
                            await pool_cm.__aexit__(*sys.exc_info())
                        except Exception:
                            pass
                    pool_cm = None

            # 池未配置或池获取失败：创建新 session
            session = await self._build_session()
            from_pool = False
            yield session, from_pool

        finally:
            # 如果不是从池中获取的，需要手动关闭
            if not from_pool and session is not None and hasattr(session, "close"):
                try:
                    await session.close()
                except Exception:
                    pass
            self._cleanup()

    '''

    # 插入新方法（在 execute() 之前）
    modified_content = content[:idx2] + new_method + content[idx2:]
    
    if modified_content == content:
        print("❌ Insertion failed")
        return False
    
    print("✅ Inserted _acquire_session() method")
    content = modified_content
    modified = True
    
    # ─────────────────────────────────────────────────────────────────────
    # 步骤 2：修改 execute() 使用新的 _acquire_session()
    # ─────────────────────────────────────────────────────────────────────
    
    # 查找 execute() 方法中的 session 获取逻辑
    # 替换为使用 async with self._acquire_session()
    
    old_session_logic = '''        pool = self._session_pool or getattr(Drone, "_session_pool", None)
        all_rounds: list[dict] = []
        final_text = ""
        _from_pool = False
        pool_context = None
        session = None

        # ── Bug-15 修复：整个执行流程必须在 `async with pool.acquire()` 范围内 ──
        # Bug-14 修复：_from_pool 标记决定 finally 是否调用 session.close()。
        try:
            if pool is not None:
                try:
                    pool_context = pool.acquire(self.drone_role)
                    pooled_session = await pool_context.__aenter__()
                    if pooled_session is not None:
                        session = pooled_session
                        _from_pool = True
                        slot = getattr(pool_context, "_slot", None)
                        sandbox_dir = getattr(slot, "sandbox_dir", None)
                        if sandbox_dir is not None:
                            self._ensure_target_link(Path(sandbox_dir))
                    else:
                        await pool_context.__aexit__(None, None, None)
                        pool_context = None
                        session = await self._build_session()
                        _from_pool = False
                except Exception:
                    if pool_context is not None:
                        try:
                            await pool_context.__aexit__(*sys.exc_info())
                        except Exception:
                            pass
                        pool_context = None
                    session = await self._build_session()
                    _from_pool = False
            else:
                session = await self._build_session()
                _from_pool = False

            # ── 构建 prompt '''

    new_session_logic = '''        all_rounds: list[dict] = []
        final_text = ""

        # ── 使用统一的 Session 管理器 ────────────────────────────────────────────
        async with self._acquire_session() as (session, from_pool):
            # ── 构建 prompt '''

    # 尝试替换
    if old_session_logic in content:
        content = content.replace(old_session_logic, new_session_logic, 1)
        print("✅ Replaced session acquisition logic in execute()")
        modified = True
    else:
        print("⚠️  Warning: Could not replace session logic (pattern not matched)")
        print("   This might be because the code has already been modified")
    
    # ─────────────────────────────────────────────────────────────────────
    # 步骤 3：简化 execute() 的 finally 块
    # ─────────────────────────────────────────────────────────────────────
    
    # 查找并移除复杂的 finally 块（现在由 _acquire_session() 处理）
    old_finally = '''        finally:
            if _from_pool and pool_context is not None:
                try:
                    await pool_context.__aexit__(*sys.exc_info())
                except Exception:
                    pass
            # Bug-14 修复：只有非池 session 才手动关闭
            if not _from_pool and session is not None and hasattr(session, "close"):
                await session.close()
            self._cleanup()'''

    # 注意：finally 块现在不再需要，因为 _acquire_session() 会自动清理
    # 但我们需要保留 _cleanup() 调用
    new_finally = '''        # Session 清理由 _acquire_session() 上下文管理器自动处理
            self._cleanup()'''

    if old_finally in content:
        content = content.replace(old_finally, new_finally, 1)
        print("✅ Simplified finally block in execute()")
        modified = True
    else:
        print("⚠️  Warning: Could not replace finally block (pattern not matched)")
    
    # ─────────────────────────────────────────────────────────────────────
    # 步骤 4：写入修复后的文件
    # ─────────────────────────────────────────────────────────────────────
    
    if not modified:
        print("❌ No modifications made")
        return False
    
    # 创建备份（如果不存在）
    backup_path = file_path + ".bak2"
    import os
    if not os.path.exists(backup_path):
        with open(backup_path, 'w', encoding='utf-8') as f:
            f.write(original)
        print(f"✅ Created backup: {backup_path}")
    
    # 写入修复后的内容
    with open(file_path, 'w', encoding='utf-8') as f:
        f.write(content)
    
    print(f"\n✅ Fixed: {file_path}")
    print("\n⚠️  Manual steps required:")
    print("   1. Review the changes")
    print("   2. Test the code: python -m py_compile engine/drone.py")
    print("   3. Run tests to verify resource cleanup")
    
    return True


if __name__ == "__main__":
    print("🔧 Fixing resource leak in engine/drone.py...")
    success = fix_drone_resource_leak()
    
    if success:
        print("\n✅ Fix applied successfully!")
    else:
        print("\n❌ Fix failed. Please check the errors above.")
        exit(1)
