#!/usr/bin/env python3
"""
定时任务 Skill - 纯数据管理，不执行调度

职责：
- 解析用户自然语言，生成定时任务 JSON
- 读写 scheduler_tasks.json 文件
- 提供任务 CRUD 接口

JSON 格式规范：
{
  "task_id_counter": 3,
  "tasks": {
    "1": {
      "id": "1",
      "chat_id": "oc_xxx",
      "execute_time": 1771526400,      // 必填，正数时间戳
      "time_interval": 60,              // 可选，重复周期（秒）
      "description": "任务描述",
      "status": "pending"               // pending/running/completed/failed
    }
  }
}

注意：
- 本 skill 只负责数据管理，不执行任务
- 任务执行由外部心跳机制（如 bot）处理
- 外部系统读取 JSON 文件，在 execute_time 执行 description
"""

import json
import os
import re
import threading
from datetime import datetime, timedelta
from typing import Dict, List, Optional


class TaskScheduler:
    """定时任务数据管理器 - 无调度功能"""
    
    def __init__(self, data_dir: str = None):
        self._lock = threading.Lock()
        
        # 数据文件路径
        if data_dir is None:
            # 默认使用工作目录（从当前目录向上查找 WORKPLACE）
            data_dir = self._find_workplace_dir()
        self.data_dir = data_dir
        self.data_file = os.path.join(data_dir, 'scheduler_tasks.json')
        
        # 确保目录存在
        os.makedirs(data_dir, exist_ok=True)
    
    def _find_workplace_dir(self) -> str:
        """
        查找工作目录
        从当前目录向上查找包含 WORKPLACE 或 .kimi 的目录
        """
        # 1. 首先尝试环境变量
        if 'CLAWDBOZ_WORKPLACE' in os.environ:
            return os.environ['CLAWDBOZ_WORKPLACE']
        
        # 2. 从当前目录向上查找
        current_dir = os.getcwd()
        for _ in range(10):  # 最多向上查找 10 层
            # 检查是否有 local_workspace 目录
            local_ws = os.path.join(current_dir, 'local_workspace')
            if os.path.isdir(local_ws):
                return local_ws
            
            # 检查是否有 WORKPLACE 目录
            workplace = os.path.join(current_dir, 'WORKPLACE')
            if os.path.isdir(workplace):
                return workplace
            
            # 检查是否有 .kimi 目录（说明是项目根目录）
            if os.path.isdir(os.path.join(current_dir, '.kimi')):
                local_ws = os.path.join(current_dir, 'local_workspace')
                os.makedirs(local_ws, exist_ok=True)
                return local_ws
            
            # 向上移动
            parent = os.path.dirname(current_dir)
            if parent == current_dir:  # 到达根目录
                break
            current_dir = parent
        
        # 3. 默认使用当前目录
        return os.getcwd()
    
    def _save_data(self, data: dict):
        """原子写入 JSON 文件"""
        try:
            with self._lock:
                temp_file = self.data_file + '.tmp'
                with open(temp_file, 'w', encoding='utf-8') as f:
                    json.dump(data, f, ensure_ascii=False, indent=2)
                os.replace(temp_file, self.data_file)
        except Exception as e:
            raise RuntimeError(f"保存任务文件失败: {e}")
    
    def _load_data(self) -> dict:
        """加载 JSON 文件，如果不存在返回空结构"""
        if not os.path.exists(self.data_file):
            return {'task_id_counter': 0, 'tasks': {}}
        
        try:
            with open(self.data_file, 'r', encoding='utf-8') as f:
                return json.load(f)
        except Exception as e:
            raise RuntimeError(f"加载任务文件失败: {e}")
    
    def create_task(self, chat_id: str, description: str, execute_time: float, 
                    time_interval: int = None) -> str:
        """
        创建定时任务
        
        Args:
            chat_id: 聊天 ID
            description: 任务描述（执行时发给 Kimi）
            execute_time: 执行时间戳（必须为正数）
            time_interval: 重复间隔（秒），None 表示一次性任务
            
        Returns:
            task_id: 任务 ID
        """
        if execute_time <= 0:
            raise ValueError("execute_time 必须是正数时间戳")
        
        data = self._load_data()
        
        # 生成新 ID
        data['task_id_counter'] += 1
        task_id = str(data['task_id_counter'])
        
        # 构建任务对象（严格遵循格式规范）
        task = {
            'id': task_id,
            'chat_id': chat_id,
            'execute_time': execute_time,
            'description': description,
            'status': 'pending'
        }
        
        # 重复任务添加 time_interval
        if time_interval is not None and time_interval > 0:
            task['time_interval'] = int(time_interval)
        
        # 保存到数据结构
        data['tasks'][task_id] = task
        self._save_data(data)
        
        return task_id
    
    def get_task(self, task_id: str) -> Optional[dict]:
        """获取单个任务"""
        data = self._load_data()
        task = data['tasks'].get(str(task_id))
        return task.copy() if task else None
    
    def list_tasks(self, chat_id: str = None, status: str = None) -> List[dict]:
        """
        列出任务
        
        Args:
            chat_id: 过滤指定聊天
            status: 过滤指定状态
            
        Returns:
            任务列表（按 execute_time 排序）
        """
        data = self._load_data()
        tasks = []
        
        for task in data['tasks'].values():
            if chat_id and task['chat_id'] != chat_id:
                continue
            if status and task['status'] != status:
                continue
            tasks.append(task.copy())
        
        # 按执行时间排序
        tasks.sort(key=lambda x: x['execute_time'])
        return tasks
    
    def update_task(self, task_id: str, **kwargs) -> bool:
        """
        更新任务字段
        
        允许更新的字段：
        - description: 任务描述
        - execute_time: 执行时间（正数）
        - time_interval: 重复间隔（None 或正数）
        - status: 状态
        """
        allowed_fields = {'description', 'execute_time', 'time_interval', 'status'}
        
        data = self._load_data()
        task_id = str(task_id)
        
        if task_id not in data['tasks']:
            return False
        
        task = data['tasks'][task_id]
        
        for key, value in kwargs.items():
            if key not in allowed_fields:
                continue
            
            if key == 'execute_time' and value <= 0:
                raise ValueError("execute_time 必须是正数")
            
            if key == 'time_interval':
                if value is None:
                    # 删除重复间隔，变一次性任务
                    task.pop('time_interval', None)
                    continue
                elif value <= 0:
                    raise ValueError("time_interval 必须是正数或 None")
            
            task[key] = value
        
        self._save_data(data)
        return True
    
    def delete_task(self, task_id: str) -> bool:
        """删除任务"""
        data = self._load_data()
        task_id = str(task_id)
        
        if task_id not in data['tasks']:
            return False
        
        del data['tasks'][task_id]
        self._save_data(data)
        return True
    
    def get_next_task_id(self) -> int:
        """获取下一个任务 ID（用于显示）"""
        data = self._load_data()
        return data['task_id_counter'] + 1
    
    def tick(self, current_time: float = None, window_start: float = None) -> List[dict]:
        """
        心跳 tick - 检查并返回需要执行的任务
        
        由外部心跳机制（如 bot）定期调用，返回需要执行的任务列表。
        执行完成后，外部需要调用 update_task() 更新任务状态。
        
        Args:
            current_time: 当前时间戳（默认 time.time()）
            window_start: 检查窗口起始时间（默认上次检查时间）
            
        Returns:
            需要执行的任务列表（pending 状态且 execute_time 在当前窗口内）
        """
        if current_time is None:
            current_time = datetime.now().timestamp()
        
        # 获取上次检查时间（存储在内存中）
        if not hasattr(self, '_last_tick_time'):
            self._last_tick_time = current_time - 60  # 默认60秒窗口
        
        if window_start is None:
            window_start = self._last_tick_time
        
        # 更新上次检查时间
        self._last_tick_time = current_time
        
        data = self._load_data()
        tasks = data.get('tasks', {})
        
        pending_tasks = []
        for task_id_str, task_data in tasks.items():
            execute_time = task_data.get('execute_time')
            status = task_data.get('status', 'pending')
            
            # 只检查 pending 状态的任务
            if status != 'pending':
                continue
            
            # 规则1: 如果 execute_time 为空，默认是有效任务（立即执行）
            if execute_time is None or execute_time == '':
                pending_tasks.append(task_data.copy())
            # 规则2: 如果执行时间在当前窗口内
            elif window_start <= execute_time <= current_time:
                pending_tasks.append(task_data.copy())
        
        return pending_tasks


# ==================== 工具函数 ====================

def parse_time(time_str: str) -> Optional[float]:
    """
    解析时间字符串为时间戳
    
    支持格式：
    - "10分钟后", "1小时后", "2天后"
    - "明天上午9点", "今天下午3点"
    - "2024-01-15 09:00"
    """
    now = datetime.now()
    time_str = time_str.strip().lower()
    
    try:
        # YYYY-MM-DD HH:MM
        try:
            dt = datetime.strptime(time_str, "%Y-%m-%d %H:%M")
            return dt.timestamp()
        except ValueError:
            pass
        
        # X分钟后
        match = re.match(r'(\d+)\s*分钟后?', time_str)
        if match:
            minutes = int(match.group(1))
            return (now + timedelta(minutes=minutes)).timestamp()
        
        # X小时后
        match = re.match(r'(\d+)\s*小时后?', time_str)
        if match:
            hours = int(match.group(1))
            return (now + timedelta(hours=hours)).timestamp()
        
        # X天后
        match = re.match(r'(\d+)\s*天后?', time_str)
        if match:
            days = int(match.group(1))
            return (now + timedelta(days=days)).timestamp()
        
        # 明天上午/下午X点
        match = re.match(r'明天\s*(上午|下午)?\s*(\d+)\s*点?', time_str)
        if match:
            am_pm = match.group(1)
            hour = int(match.group(2))
            if am_pm == '下午' and hour < 12:
                hour += 12
            dt = now + timedelta(days=1)
            dt = dt.replace(hour=hour, minute=0, second=0, microsecond=0)
            return dt.timestamp()
        
        # 今天/下午X点
        match = re.match(r'(今天)?\s*(上午|下午)?\s*(\d+)\s*点?', time_str)
        if match:
            am_pm = match.group(2)
            hour = int(match.group(3))
            if am_pm == '下午' and hour < 12:
                hour += 12
            dt = now.replace(hour=hour, minute=0, second=0, microsecond=0)
            if dt < now:
                dt += timedelta(days=1)
            return dt.timestamp()
        
        return None
        
    except Exception:
        return None


def parse_interval(interval_str: str) -> Optional[int]:
    """
    解析重复间隔为秒数
    
    支持格式：
    - "每X分钟", "X分钟一次"
    - "每X小时", "X小时一次"
    - "每X天", "X天一次"
    """
    interval_str = interval_str.strip().lower()
    
    match = re.match(r'(?:每\s*)?(\d+)\s*分钟(?:一次)?', interval_str)
    if match:
        return int(match.group(1)) * 60
    
    match = re.match(r'(?:每\s*)?(\d+)\s*小时(?:一次)?', interval_str)
    if match:
        return int(match.group(1)) * 3600
    
    match = re.match(r'(?:每\s*)?(\d+)\s*天(?:一次)?', interval_str)
    if match:
        return int(match.group(1)) * 86400
    
    return None


def format_task_list(tasks: List[dict]) -> str:
    """格式化任务列表为可读文本"""
    if not tasks:
        return "暂无定时任务"
    
    lines = ["📋 **定时任务列表**\n"]
    
    for task in tasks:
        task_id = task['id']
        desc = task['description']
        exec_time = task['execute_time']
        is_recurring = task.get('time_interval') is not None
        
        # 图标
        icon = "🔄" if is_recurring else "⏰"
        
        # 时间
        dt = datetime.fromtimestamp(exec_time)
        time_str = dt.strftime("%m-%d %H:%M")
        
        # 重复信息
        repeat_info = ""
        if is_recurring:
            interval = task['time_interval']
            if interval < 3600:
                repeat_info = f" (每{interval//60}分)"
            elif interval < 86400:
                repeat_info = f" (每{interval//3600}小时)"
            else:
                repeat_info = f" (每{interval//86400}天)"
        
        # 状态
        status_emoji = {
            'pending': '⏳',
            'running': '▶️',
            'completed': '✅',
            'failed': '❌'
        }.get(task['status'], '❓')
        
        lines.append(f"{icon} **#{task_id}** {time_str}{repeat_info} {status_emoji}")
        lines.append(f"   {desc[:30]}{'...' if len(desc) > 30 else ''}\n")
    
    return "\n".join(lines)


def format_task_detail(task: dict) -> str:
    """格式化任务详情"""
    task_id = task['id']
    desc = task['description']
    exec_time = task['execute_time']
    is_recurring = task.get('time_interval') is not None
    
    dt = datetime.fromtimestamp(exec_time)
    time_str = dt.strftime("%Y-%m-%d %H:%M:%S")
    
    lines = [f"📋 **任务 #{task_id}**\n"]
    
    # 类型
    if is_recurring:
        interval = task['time_interval']
        lines.append(f"🔄 **类型:** 重复任务 (每 {interval} 秒)")
    else:
        lines.append(f"⏰ **类型:** 一次性任务")
    
    # 状态
    status = task['status']
    status_text = {
        'pending': '⏳ 等待执行',
        'running': '▶️ 执行中',
        'completed': '✅ 已完成',
        'failed': '❌ 失败'
    }.get(status, status)
    lines.append(f"**状态:** {status_text}")
    
    # 时间
    lines.append(f"📅 **执行时间:** {time_str}")
    
    # 聊天ID（简化）
    chat_id = task['chat_id']
    short_id = chat_id[:10] + "..." if len(chat_id) > 10 else chat_id
    lines.append(f"💬 **聊天:** {short_id}")
    
    # 描述
    lines.append(f"\n📝 **任务内容:**\n```\n{desc}\n```")
    
    return "\n".join(lines)


# 全局实例
_scheduler: Optional[TaskScheduler] = None


def get_scheduler(data_dir: str = None) -> TaskScheduler:
    """获取全局调度器实例"""
    global _scheduler
    if _scheduler is None:
        _scheduler = TaskScheduler(data_dir)
    elif data_dir is not None:
        _scheduler.data_dir = data_dir
        _scheduler.data_file = os.path.join(data_dir, 'scheduler_tasks.json')
    return _scheduler


# ==================== 便捷函数 ====================

def create_task(chat_id: str, description: str, execute_time: float, 
                time_interval: int = None, data_dir: str = None) -> str:
    """创建定时任务"""
    return get_scheduler(data_dir).create_task(chat_id, description, execute_time, time_interval)

def get_task(task_id: str, data_dir: str = None) -> Optional[dict]:
    """获取单个任务"""
    return get_scheduler(data_dir).get_task(task_id)

def list_tasks(chat_id: str = None, status: str = None, data_dir: str = None) -> List[dict]:
    """列出任务"""
    return get_scheduler(data_dir).list_tasks(chat_id, status)

def update_task(task_id: str, data_dir: str = None, **kwargs) -> bool:
    """更新任务"""
    return get_scheduler(data_dir).update_task(task_id, **kwargs)

def delete_task(task_id: str, data_dir: str = None) -> bool:
    """删除任务"""
    return get_scheduler(data_dir).delete_task(task_id)

def tick(current_time: float = None, window_start: float = None, data_dir: str = None) -> List[dict]:
    """
    心跳 tick - 检查并返回需要执行的任务
    
    Args:
        current_time: 当前时间戳（默认 time.time()）
        window_start: 检查窗口起始时间
        data_dir: 数据目录
        
    Returns:
        需要执行的任务列表
    """
    return get_scheduler(data_dir).tick(current_time, window_start)


# ==================== 使用示例 ====================

if __name__ == "__main__":
    import tempfile
    import shutil
    
    # 创建临时目录测试
    test_dir = tempfile.mkdtemp()
    print(f"测试目录: {test_dir}")
    
    try:
        scheduler = TaskScheduler(test_dir)
        
        # 创建一次性任务
        task1 = scheduler.create_task(
            chat_id="oc_test001",
            description="明天上午9点提醒开会",
            execute_time=(datetime.now() + timedelta(days=1)).replace(hour=9, minute=0).timestamp()
        )
        print(f"✅ 创建一次性任务: #{task1}")
        
        # 创建重复任务
        task2 = scheduler.create_task(
            chat_id="oc_test001",
            description="每5分钟获取纳斯达克指数",
            execute_time=(datetime.now() + timedelta(minutes=1)).timestamp(),
            time_interval=300
        )
        print(f"✅ 创建重复任务: #{task2}")
        
        # 列出任务
        tasks = scheduler.list_tasks()
        print(f"\n📋 任务列表:\n{format_task_list(tasks)}")
        
        # 查看生成的 JSON
        print("\n📄 生成的 scheduler_tasks.json:")
        with open(os.path.join(test_dir, 'scheduler_tasks.json'), 'r') as f:
            print(f.read())
        
        # 更新任务
        scheduler.update_task(task1, status='running')
        print(f"✅ 更新任务 #{task1} 状态为 running")
        
        # 获取详情
        task = scheduler.get_task(task2)
        print(f"\n📋 任务 #{task2} 详情:\n{format_task_detail(task)}")
        
        # 删除任务
        scheduler.delete_task(task1)
        print(f"✅ 删除任务 #{task1}")
        
        # 最终列表
        print(f"\n📋 最终任务数: {len(scheduler.list_tasks())}")
        
    finally:
        shutil.rmtree(test_dir)
        print(f"\n清理测试目录")
