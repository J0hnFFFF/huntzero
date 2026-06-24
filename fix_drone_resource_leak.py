#!/usr/bin/env python3
"""
修复 engine/drone.py 中的资源泄漏问题。

问题：
1. execute() 中手动调用 __aenter__()/__aexit__() 容易出错
2. _from_pool 标志位逻辑复杂，容易遗漏资源释放
3. 异常情况下 Session 可能未正确关闭

修复方案：
添加 _acquire_session() 异步上下文管理器，统一 Session 生命周期管理。
"""

import re
import sys
from pathlib import Path

def fix_drone_resource_leak():
    file_path = Path(__file__).parent / "engine" / "drone.py"
    
    if not file_path.exists():
        print(f"❌ File not found: {file_path}")
        return False
    
    content = file_path.read_text(encoding="utf-8")
    original_content = content
    
    # ─────────────────────────────────────────────────────────────────────────────
    # 步骤 1：在 execute() 方法之前插入 _acquire_session() 方法
    # ─────────────────────────────────────────────────────────────────────────────
    
    # 找到 "执行入口" 注释和 execute() 方法定义
    # 使用正则表达式匹配（允许空白字符变化）
    pattern = r'(\s+#\s+─+\s+执行入口\s+─+\s*\n\n\s+async def execute\(self\) -> str:)'
    
    # 新的 _acquire_session() 方法（使用制表符缩进）
    new_method = '''    # ── Session 获取上下文管理器 ──────────────────────────────────────────────────────────

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

    # ── 执行入口 ─────────────────────────────────────────────────────────────────────

    async def execute(self) -> str:
'''
    
    # 执行替换
    replacement = r'\1'.replace(r'\1', new_method)
    
    # 使用更灵活的匹配
    lines = content.split('\n')
    insert_idx = None
    for i, line in enumerate(lines):
        if 'async def execute(self) -> str:' in line:
            # 向前查找 "执行入口" 注释
            for j in range(i-1, max(i-5, -1), -1):
                if '执行入口' in lines[j]:
                    insert_idx = j
                    break
            break
    
    if insert_idx is None:
        print("❌ Could not find insertion point for _acquire_session()")
        return False
    
    print(f"✅ Found insertion point at line {insert_idx + 1}")
    
    # 插入新方法
    lines.insert(insert_idx, new_method)
    content = '\n'.join(lines)
    
    # ─────────────────────────────────────────────────────────────────────────────
    # 步骤 2：修改 execute() 使用新的 _acquire_session()
    # ─────────────────────────────────────────────────────────────────────────────
    
    # 找到 execute() 方法的开头部分（try: 之前）
    # 替换为使用 async with self._acquire_session()
    
    # 由于 execute() 方法很长，我们采用策略性替换：
    # 1. 删除旧的 pool 获取逻辑
    # 2. 添加 async with self._acquire_session() as (session, from_pool):
    
    # 使用正则表达式匹配 execute() 中的池逻辑部分
    old_pool_logic = r'''        pool = self\._session_pool or getattr\(Drone, "_session_pool", None\)
        all_rounds: list\[dict\] = \[\]
        final_text = ""
        _from_pool = False
        pool_context = None
        session = None

        # ── Bug-15 修复：整个执行流程必须在 `async with pool\.acquire\(\)` 范围内 ──
        # Bug-14 修复：_from_pool 标记决定 finally 是否调用 session\.close\(\)\。
        try:
            if pool is not None:
                try:
                    pool_context = pool\.acquire\(self\.drone_role\)
                    pooled_session = await pool_context\.__aenter__\(\)
                    if pooled_session is not None:
                        session = pooled_session
                        _from_pool = True
                        slot = getattr\(pool_context, "_slot", None\)
                        sandbox_dir = getattr\(slot, "sandbox_dir", None\)
                        if sandbox_dir is not None:
                            self\._ensure_target_link\(Path\(sandbox_dir\)\)
                    else:
                        await pool_context\.__aexit__\(None, None, None\)
                        pool_context = None
                        session = await self\._build_session\(\)
                        _from_pool = False
                except Exception:
                    if pool_context is not None:
                        try:
                            await pool_context\.__aexit__\(\*sys\.exc_info\(\)\)
                        except Exception:
                            pass
                        pool_context = None
                    session = await self\._build_session\(\)
                    _from_pool = False
            else:
                session = await self\._build_session\(\)
                _from_pool = False'''

    new_session_logic = r'''        all_rounds: list[dict] = []
        final_text = ""

        # ── 使用统一的 Session 管理器 ──
        async with self._acquire_session() as (session, from_pool):'''

    # 尝试替换（使用 re.DOTALL 标志）
    new_content = re.sub(
        old_pool_logic,
        new_session_logic,
        content,
        flags=re.DOTALL | re.MULTILINE
    )
    
    if new_content == content:
        print("⚠️  Warning: Could not replace old pool logic (pattern not matched)")
        print("   This might be because the code has already been modified")
        # 不返回 False，继续尝试修复 finally 块
    else:
        print("✅ Replaced old pool logic with new _acquire_session() usage")
        content = new_content
    
    # ─────────────────────────────────────────────────────────────────────────────
    # 步骤 3：修改 finally 块，移除复杂的 _from_pool 逻辑
    # ─────────────────────────────────────────────────────────────────────────────
    
    old_finally = r'''        finally:
            if _from_pool and pool_context is not None:
                try:
                    await pool_context\.__aexit__\(\*sys\.exc_info\(\)\)
                except Exception:
                    pass
            # Bug-14 修复：只有非池 session 才手动关闭
            if not _from_pool and session is not None and hasattr\(session, "close"\):
                await session\.close\(\)
            self\._cleanup\(\)'''

    new_finally = r'''        # finally 块不再需要，因为 _acquire_session() 上下文管理器会自动清理'''
    
    new_content = re.sub(
        old_finally,
        new_finally,
        content,
        flags=re.DOTALL | re.MULTILINE
    )
    
    if new_content == content:
        print("⚠️  Warning: Could not replace finally block (pattern not matched)")
    else:
        print("✅ Simplified finally block")
        content = new_content
    
    # ─────────────────────────────────────────────────────────────────────────────
    # 步骤 4：移除 execute() 末尾的 session 关闭逻辑
    # （现在由 _acquire_session() 自动处理）
    # ─────────────────────────────────────────────────────────────────────────────
    
    # 查找并移除末尾的 session 关闭代码（在 return final_text 之前）
    old_cleanup = r'''        if len\(all_rounds\) > 1:
            final_text = self\._synthesize_multi_round\(all_rounds, final_text\)

        return final_text'''
    
    new_cleanup = r'''        if len(all_rounds) > 1:
            final_text = self._synthesize_multi_round(all_rounds, final_text)

        return final_text'''
    
    # 这个不需要修改，因为 session 关闭现在在上下文管理器中处理
    
    # ─────────────────────────────────────────────────────────────────────────────
    # 写入修复后的文件
    # ─────────────────────────────────────────────────────────────────────────────
    
    backup_path = file_path.with_suffix('.py.bak')
    if not backup_path.exists():
        backup_path.write_text(original_content, encoding="utf-8")
        print(f"✅ Created backup: {backup_path}")
    
    file_path.write_text(content, encoding="utf-8")
    print(f"✅ Fixed: {file_path}")
    
    return True

if __name__ == "__main__":
    print("🔧 Fixing resource leak in engine/drone.py...")
    success = fix_drone_resource_leak()
    
    if success:
        print("\n✅ Fix applied successfully!")
        print("\n⚠️  Manual steps required:")
        print("   1. Review the changes")
        print("   2. Test the code: python -m py_compile engine/drone.py")
        print("   3. Run tests to verify resource cleanup")
    else:
        print("\n❌ Fix failed. Please check the errors above.")
        sys.exit(1)
