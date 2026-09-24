#!/usr/bin/env bash
# build_cython.sh — 将 engine/ 与 tools/ 的 .py 逐模块编译为 Cython .so。
#
# 产物布局（build/compiled-pkg/，已被 .gitignore 覆盖）：
#   engine/__init__.py            只复制（包入口）
#   engine/<module>.cpython-*.so  逐模块编译
#   engine/semantic_helper_core*.so + engine/semantic_helper.py（薄启动器）
#   tools/<module>.cpython-*.so   逐模块编译（tools/ 无 __init__.py，命名空间包）
#
# 用法：bash tools/build_cython.sh
#   PYTHON=/path/to/python  指定解释器（默认 python3）
#   CC=gcc                  指定 C 编译器（默认 gcc）
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PY="${PYTHON:-python3}"
CC="${CC:-gcc}"
OUT="$ROOT/build/compiled-pkg"
C_DIR="$ROOT/build/c-src"

# ── 工具链预检（缺失即明确报错，不伪装成模块编译失败）──
if ! command -v "$PY" >/dev/null 2>&1; then
    echo "ERROR: python interpreter not found: $PY (set PYTHON=...)" >&2
    exit 2
fi
if ! command -v "$CC" >/dev/null 2>&1; then
    echo "ERROR: C compiler not found: $CC (set CC=...)" >&2
    exit 2
fi
if ! "$PY" -c "import Cython" >/dev/null 2>&1; then
    echo "ERROR: Cython not installed for $PY (pip install cython)" >&2
    exit 2
fi

PY_INC="$("$PY" -c 'import sysconfig; print(sysconfig.get_paths()["include"])')"
EXT_SUFFIX="$("$PY" -c 'import sysconfig; print(sysconfig.get_config_var("EXT_SUFFIX"))')"

rm -rf "$OUT" "$C_DIR"
mkdir -p "$OUT/engine" "$OUT/tools" "$C_DIR"

# compile_module <src.py> <module-name> <out-dir> <stem>
compile_module() {
    local src="$1" modname="$2" outdir="$3" stem="$4"
    local cfile="$C_DIR/${stem}.c"
    echo "[cython] $modname"
    "$PY" -m cython --module-name "$modname" -3 "$src" -o "$cfile"
    echo "[gcc]    ${stem}${EXT_SUFFIX}"
    "$CC" -shared -fPIC -O2 -I"$PY_INC" "$cfile" -o "$outdir/${stem}${EXT_SUFFIX}"
}

# ── engine/：__init__.py 只复制，其余逐模块编译 ──
cp "$ROOT/engine/__init__.py" "$OUT/engine/__init__.py"
for src in "$ROOT"/engine/*.py; do
    stem="$(basename "$src" .py)"
    case "$stem" in
        __init__|semantic_helper) continue ;;
    esac
    compile_module "$src" "engine.$stem" "$OUT/engine" "$stem"
done

# semantic_helper：编为 semantic_helper_core，另附薄启动器保持
# `python engine/semantic_helper.py <cmd> <args>` 调用方式不变。
compile_module "$ROOT/engine/semantic_helper.py" "engine.semantic_helper_core" \
    "$OUT/engine" "semantic_helper_core"
cat > "$OUT/engine/semantic_helper.py" <<'LAUNCHER'
#!/usr/bin/env python3
"""semantic_helper 薄启动器（Cython 交付形态）。

实现编译在同目录 semantic_helper_core（.so）中；本启动器仅保持
`python engine/semantic_helper.py <cmd> <args>` 的直接脚本调用方式，
分发逻辑与源码版 engine/semantic_helper.py 的 __main__ 块逐字一致。
.so 按路径直接加载，不触发 engine 包导入（避免 SDK 依赖与告警噪音）。
"""

import importlib.util
import json
import sys
from pathlib import Path

_so = next(iter(sorted(Path(__file__).parent.glob("semantic_helper_core*.so"))))
_spec = importlib.util.spec_from_file_location("semantic_helper_core", _so)
_core = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_core)

list_functions = _core.list_functions
find_references = _core.find_references
get_call_graph = _core.get_call_graph
get_function_body = _core.get_function_body
_run_analysis_command = _core._run_analysis_command

if __name__ == "__main__":
    if len(sys.argv) < 3:
        print("Usage: python semantic_helper.py <command> <args...>")
        print("Commands:")
        print("  list_functions <file>")
        print("  find_references <symbol> <directory>")
        print("  get_call_graph <file>")
        print("  get_function_body <file> <func_name>")
        print("  ast_query -t <target> -l <lang> -q <query>")
        print("  call_graph -t <target> -l <lang> -f <function> [-d direction]")
        print("  scope_extract -t <target> -l <lang> -n <name>")
        print("  data_flow -t <target> -l <lang> -s <source> -k <sink> [-d max_depth]")
        sys.exit(1)

    cmd = sys.argv[1]
    if cmd == "list_functions":
        result = list_functions(sys.argv[2])
    elif cmd == "find_references":
        result = find_references(sys.argv[2], sys.argv[3])
    elif cmd == "get_call_graph":
        result = get_call_graph(sys.argv[2])
    elif cmd == "get_function_body":
        result = get_function_body(sys.argv[2], sys.argv[3])
    elif cmd in ("ast_query", "call_graph", "scope_extract", "data_flow"):
        _run_analysis_command(cmd, sys.argv[2:])
        sys.exit(0)
    else:
        print(f"Unknown command: {cmd}")
        sys.exit(1)

    print(json.dumps(result, indent=2, ensure_ascii=False))
LAUNCHER

# ── tools/：无 __init__.py（命名空间包），逐模块编译 ──
for src in "$ROOT"/tools/*.py; do
    stem="$(basename "$src" .py)"
    compile_module "$src" "tools.$stem" "$OUT/tools" "$stem"
done

echo "BUILD OK -> $OUT"
