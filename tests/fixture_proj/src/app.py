"""fixture：故意埋缺陷的小目标，供集成测试扫描。"""

import ctypes


def parse_header(data: bytes) -> bytes:
    """从 data 读取长度前缀并拷贝到固定缓冲区（故意缺边界检查）。"""
    length = int.from_bytes(data[:4], "little")
    buf = ctypes.create_string_buffer(256)
    ctypes.memmove(buf, data[4 : 4 + length], length)  # length 未校验 → 溢出
    return buf.raw
