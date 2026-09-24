"""engine/cerebrum.py `_compute_sector_budget` 扇区预算映射（registry #4 回归）。

五组映射逐条断言（修复后公式：max(40,min(120,files*0.8))*multiplier /
max(10,min(25,tasks//4))，multiplier = {0:1.5, 1:1.2, 2:1.0, 3:0.8}）。
"""

import pytest

from engine.cerebrum import _compute_sector_budget


class TestComputeSectorBudget:
    @pytest.mark.parametrize(
        "file_count, priority, expected",
        [
            (800, 0, (180, 25)),  # 640→120 上限 ×1.5；45→25 上限
            (100, 1, (96, 24)),  # 80 ×1.2
            (50, 2, (40, 10)),  # 40 下限 ×1.0
            (30, 3, (32, 10)),  # 24→40 下限 ×0.8；8→10 下限
            (200, 0, (180, 25)),  # 160→120 上限 ×1.5
        ],
    )
    def test_compute_sector_budget(self, file_count, priority, expected):
        assert _compute_sector_budget(file_count, priority) == expected
