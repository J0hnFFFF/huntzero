# Cython 编译 spike 报告（2026-09-25）

## 结论

**主路线可行** —— engine/ + tools/ 全部 15 个模块编译成功，双跑（源码版 vs 编译版）
pytest 结果完全一致，Phase 2 交付走 Cython 逐模块编译。

## 构建参数

`tools/build_cython.sh`（`set -euo pipefail`；`command -v` 预检 python/gcc，
`$PY -c "import Cython"` 预检，缺失即 exit 2）：

```bash
PYTHON=/home/fuzz/miniconda3/envs/sec/bin/python bash tools/build_cython.sh
# 逐模块：
$PY -m cython --module-name <pkg>.<mod> -3 <src>.py -o build/c-src/<mod>.c
gcc -shared -fPIC -O2 -I$(sysconfig include) <mod>.c -o build/compiled-pkg/<pkg>/<mod>$(EXT_SUFFIX)
```

环境：Python 3.13.0（miniconda envs/sec）、Cython 3.2.9、gcc 13.3.0（Ubuntu 24.04）。

产物布局（`build/compiled-pkg/`，共 4.6M，gitignored）：

- `engine/__init__.py`：只复制不编译（包入口）
- `engine/` 10 个 .so：backends / blackboard / cerebrum / drone / fuzzer /
  llm_config / osv_bridge / sector / session_pool / semantic_helper_core
- `engine/semantic_helper.py`：薄启动器（见下）
- `tools/` 5 个 .so：check_env_sync / exploit_analyzer / finops_monitor /
  install_osv / poc_sandbox（tools/ 无 `__init__.py`，命名空间包语义不变）

**semantic_helper 特殊处理**：实现编为 `engine.semantic_helper_core`（.so），
另生成同名薄启动器 `engine/semantic_helper.py` 保持
`python engine/semantic_helper.py <cmd> <args>` 直接脚本调用（skills 与
`.bots.md` 依赖此形态）。启动器用 `importlib.util.spec_from_file_location`
按路径直载同目录 `semantic_helper_core*.so`，不触发 engine 包导入——避免
SDK 依赖与第三方告警（authlib deprecation）污染 stdout/stderr，且无需
PYTHONPATH 即可运行。分发逻辑与源码 `__main__` 块逐字一致。

## 双跑结果

| 运行 | 命令 | 结果 |
|---|---|---|
| 源码版 | `python -m pytest tests -q` | **100 passed, 1 warning in ~3.5s** |
| 编译版 | `cd /tmp && PYTHONPATH=build/compiled-pkg:/home/fuzz/huntzero pytest /home/fuzz/huntzero/tests -q` | **100 passed, 1 warning in ~3.4s** |

编译版加载证据：`engine.blackboard.__file__` / `engine.cerebrum.__file__` /
`tools.poc_sandbox.__file__` 均指向 `build/compiled-pkg/**.cpython-313-x86_64-linux-gnu.so`。

薄启动器 parity（stdout + exit code 逐项 diff，7 组全部 IDENTICAL）：
`list_functions` / `find_references` / `get_call_graph` / `get_function_body` /
`call_graph -t -l -f` / `data_flow -t -l -s -k` / `ast_query -t -l -q`
（fixture：tests/fixture_proj/src/app.py）；直跑 stderr 0 行。

导入耗时参考：`import engine.cerebrum`（编译版）~0.94s（含 SDK 依赖链）。

## 已知问题与外置资源

- `skills/` 是运行时按路径读取的数据目录，不编译，需随交付外置。
- `bin/osv-scanner` 外部二进制依赖，不编译（`OSV_SCANNER_PATH` 或自动探测）。
- `__pycache__/` 会在 compiled-pkg 内生成（import `__init__.py` 时），无害。
- 顶层模块 `huntzero.py` / `kimi_hive.py` / `kimi_sdk_compat.py` 未纳入编译
  （入口层，保持源码分发）；`tests/` 不编译。
- 编译版与源码版同机同解释器验证；跨机分发需同 Python minor 版本与 ABI
  （EXT_SUFFIX 钉死 `cpython-313-x86_64-linux-gnu`）。
- 真实 docker 端到端（poc_sandbox docker 模式）以 mock 契约为准，未在编译版复验。
