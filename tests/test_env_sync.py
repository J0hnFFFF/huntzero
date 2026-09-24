"""SPEC 5.2：.env.example 必须与代码 env 读取点一致（防再脱节）。"""

from pathlib import Path

from tools.check_env_sync import check_env_sync

REPO_ROOT = Path(__file__).resolve().parent.parent


def test_env_example_covers_all_code_reads():
    missing_in_example, ghosts_in_example = check_env_sync(REPO_ROOT)
    assert missing_in_example == set(), f".env.example 缺少: {missing_in_example}"
    assert ghosts_in_example == set(), f".env.example 幽灵变量: {ghosts_in_example}"
