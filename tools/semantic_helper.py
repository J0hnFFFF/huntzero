"""
tools/semantic_helper.py — kimiSec 语义级代码分析辅助工具。

Drone 通过终端命令调用此脚本，获取 AST 查询、调用图和数据流分析结果。
使用 Tree-sitter 作为解析后端，支持多语言、零运行时依赖。

安装依赖：
  pip install tree-sitter tree-sitter-python tree-sitter-javascript
  pip install tree-sitter-typescript tree-sitter-c tree-sitter-go

支持语言：python | javascript | typescript | c | cpp | go | rust | java
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path
from typing import Optional


# ─────────────────────────────────────────────────────────────────────────────
#  语言到 tree-sitter 包的映射
# ─────────────────────────────────────────────────────────────────────────────

LANG_MAP = {
    "python":     ("tree_sitter_python",     "python"),
    "javascript": ("tree_sitter_javascript", "javascript"),
    "typescript": ("tree_sitter_typescript", "typescript"),
    "c":          ("tree_sitter_c",          "c"),
    "cpp":        ("tree_sitter_cpp",        "cpp"),
    "go":         ("tree_sitter_go",         "go"),
    "rust":       ("tree_sitter_rust",       "rust"),
    "java":       ("tree_sitter_java",       "java"),
}

LANG_EXTENSIONS = {
    "python":     [".py"],
    "javascript": [".js", ".mjs", ".cjs"],
    "typescript": [".ts", ".tsx"],
    "c":          [".c", ".h"],
    "cpp":        [".cpp", ".cc", ".cxx", ".hpp"],
    "go":         [".go"],
    "rust":       [".rs"],
    "java":       [".java"],
}


def _load_language(lang_name: str):
    """动态加载 tree-sitter 语言模块。"""
    try:
        import tree_sitter
        pkg, attr = LANG_MAP[lang_name]
        mod = __import__(pkg)
        language = tree_sitter.Language(mod.language())
        return language
    except ImportError:
        print(json.dumps({
            "error": f"Tree-sitter language '{lang_name}' not installed. "
                     f"Run: pip install tree-sitter tree-sitter-{lang_name.replace('_', '-')}"
        }))
        sys.exit(1)
    except KeyError:
        print(json.dumps({"error": f"Unsupported language: {lang_name}. "
                                   f"Supported: {list(LANG_MAP)}"}))
        sys.exit(1)


def _collect_files(target: str, lang: str) -> list[Path]:
    """收集目标路径下所有指定语言的源文件。"""
    p = Path(target)
    exts = LANG_EXTENSIONS.get(lang, [])
    if p.is_file():
        return [p]
    files = []
    for ext in exts:
        files.extend(p.rglob(f"*{ext}"))
    # 排除常见的无关目录
    exclude = {".git", "node_modules", "__pycache__", ".venv", "vendor", "dist", "build"}
    return [f for f in files if not any(part in exclude for part in f.parts)]


def _parse_file(path: Path, parser) -> Optional[object]:
    """解析单个文件，返回语法树；解析失败时返回 None。"""
    try:
        src = path.read_bytes()
        tree = parser.parse(src)
        return tree, src
    except Exception:
        return None, None


# ─────────────────────────────────────────────────────────────────────────────
#  子命令：ast_query
# ─────────────────────────────────────────────────────────────────────────────

def cmd_ast_query(args):
    import tree_sitter
    language = _load_language(args.lang)
    parser   = tree_sitter.Parser(language)
    files    = _collect_files(args.target, args.lang)

    try:
        query = language.query(args.query)
    except Exception as e:
        print(json.dumps({"error": f"Invalid query: {e}"}))
        sys.exit(1)

    results = []
    for fpath in files:
        tree, src = _parse_file(fpath, parser)
        if tree is None:
            continue
        captures = query.captures(tree.root_node)
        for capture_name, nodes in captures.items():
            for node in nodes:
                text = src[node.start_byte:node.end_byte].decode("utf-8", errors="replace")
                results.append({
                    "file":         str(fpath),
                    "capture":      capture_name,
                    "start_line":   node.start_point[0] + 1,
                    "end_line":     node.end_point[0] + 1,
                    "text":         text[:500],         # 限制长度防止 LLM 上下文爆炸
                })

    print(json.dumps({"count": len(results), "matches": results[:100]},
                     ensure_ascii=False, indent=2))


# ─────────────────────────────────────────────────────────────────────────────
#  子命令：call_graph
# ─────────────────────────────────────────────────────────────────────────────

def cmd_call_graph(args):
    """
    简单的静态调用图：找出给定函数名的所有调用者/被调用者。
    当前实现为 Pattern-based（基于 AST 文本匹配），
    未来可升级为完整的指针分析。
    """
    import tree_sitter

    language = _load_language(args.lang)
    parser   = tree_sitter.Parser(language)
    files    = _collect_files(args.target, args.lang)
    fn_name  = args.function

    call_site_queries = {
        "python":     f'(call function: [(identifier) @fn (attribute attribute: (identifier) @fn)] (#eq? @fn "{fn_name}"))',
        "javascript": f'(call_expression function: [(identifier) @fn (member_expression property: (property_identifier) @fn)] (#eq? @fn "{fn_name}"))',
        "typescript": f'(call_expression function: [(identifier) @fn (member_expression property: (property_identifier) @fn)] (#eq? @fn "{fn_name}"))',
        "go":         f'(call_expression function: [(identifier) @fn (selector_expression field: (field_identifier) @fn)] (#eq? @fn "{fn_name}"))',
        "java":       f'(method_invocation name: (identifier) @fn (#eq? @fn "{fn_name}"))',
        "rust":       f'(call_expression function: [(identifier) @fn (path_expression (path_identifier) @fn)] (#eq? @fn "{fn_name}"))',
        "c":          f'(call_expression function: (identifier) @fn (#eq? @fn "{fn_name}"))',
        "cpp":        f'(call_expression function: [(identifier) @fn (field_expression field: (field_identifier) @fn)] (#eq? @fn "{fn_name}"))',
    }
    def_queries = {
        "python":     f'(function_definition name: (identifier) @fn (#eq? @fn "{fn_name}")) @def',
        "javascript": f'[(function_declaration name: (identifier) @fn (#eq? @fn "{fn_name}")) (method_definition name: (property_identifier) @fn (#eq? @fn "{fn_name}"))] @def',
        "typescript": f'[(function_declaration name: (identifier) @fn (#eq? @fn "{fn_name}")) (method_definition name: (property_identifier) @fn (#eq? @fn "{fn_name}")) (method_definition name: (identifier) @fn (#eq? @fn "{fn_name}"))] @def',
        "go":         f'[(function_declaration name: (identifier) @fn (#eq? @fn "{fn_name}")) (method_declaration name: (field_identifier) @fn (#eq? @fn "{fn_name}"))] @def',
        "java":       f'[(method_declaration name: (identifier) @fn (#eq? @fn "{fn_name}")) (constructor_declaration name: (identifier) @fn (#eq? @fn "{fn_name}"))] @def',
        "rust":       f'(function_item name: (identifier) @fn (#eq? @fn "{fn_name}")) @def',
        "c":          f'(function_declaration declarator: (identifier) @fn (#eq? @fn "{fn_name}")) @def',
        "cpp":        f'[(function_declaration declarator: (identifier) @fn (#eq? @fn "{fn_name}")) (function_definition declarator: (identifier) @fn (#eq? @fn "{fn_name}")) (method_declaration name: (field_identifier) @fn (#eq? @fn "{fn_name}"))] @def',
    }

    callers, callees, definitions = [], [], []
    call_q_str = call_site_queries.get(args.lang)
    def_q_str  = def_queries.get(args.lang)

    for fpath in files:
        tree, src = _parse_file(fpath, parser)
        if tree is None:
            continue

        if call_q_str and args.direction in ("callers", "both"):
            try:
                q = language.query(call_q_str)
                caps = q.captures(tree.root_node)
                for _, nodes in caps.items():
                    for node in nodes:
                        text = src[node.start_byte:node.end_byte].decode("utf-8", errors="replace")
                        callers.append({"file": str(fpath), "line": node.start_point[0]+1, "text": text[:200]})
            except Exception:
                pass

        if def_q_str:
            try:
                q = language.query(def_q_str)
                caps = q.captures(tree.root_node)
                for _, nodes in caps.items():
                    for node in nodes:
                        text = src[node.start_byte:node.end_byte].decode("utf-8", errors="replace")
                        definitions.append({"file": str(fpath), "line": node.start_point[0]+1, "text": text[:300]})
            except Exception:
                pass

    print(json.dumps({
        "function":    fn_name,
        "definitions": definitions[:20],
        "callers":     callers[:50],
    }, ensure_ascii=False, indent=2))


# ─────────────────────────────────────────────────────────────────────────────
#  子命令：scope_extract
# ─────────────────────────────────────────────────────────────────────────────

def cmd_scope_extract(args):
    """提取函数或类的完整 AST 代码段（带精确行列范围）。"""
    import tree_sitter

    language = _load_language(args.lang)
    parser   = tree_sitter.Parser(language)
    fpath    = Path(args.target)

    if not fpath.is_file():
        # 若是目录，搜索第一个包含该名称的文件
        candidates = _collect_files(args.target, args.lang)
        fpath = next((f for f in candidates if args.name in f.read_text(errors="replace")), None)
        if fpath is None:
            print(json.dumps({"error": f"Symbol '{args.name}' not found in {args.target}"}))
            return

    tree, src = _parse_file(fpath, parser)
    if tree is None:
        print(json.dumps({"error": f"Failed to parse {fpath}"}))
        return

    q_str = {
        "python":     f'[(function_definition name: (identifier) @n (#eq? @n "{args.name}")) (class_definition name: (identifier) @n (#eq? @n "{args.name}"))] @scope',
        "javascript": f'[(function_declaration name: (identifier) @n (#eq? @n "{args.name}")) (class_declaration name: (identifier) @n (#eq? @n "{args.name}"))] @scope',
        "typescript": f'[(function_declaration name: (identifier) @n (#eq? @n "{args.name}")) (class_declaration name: (identifier) @n (#eq? @n "{args.name}")) (interface_declaration name: (identifier) @n (#eq? @n "{args.name}")) (type_alias_declaration name: (identifier) @n (#eq? @n "{args.name}"))] @scope',
        "go":         f'[(function_declaration name: (identifier) @n (#eq? @n "{args.name}")) (method_declaration name: (field_identifier) @n (#eq? @n "{args.name}"))] @scope',
        "java":       f'[(class_declaration name: (identifier) @n (#eq? @n "{args.name}")) (interface_declaration name: (identifier) @n (#eq? @n "{args.name}")) (method_declaration name: (identifier) @n (#eq? @n "{args.name}"))] @scope',
        "rust":       f'[(function_item name: (identifier) @n (#eq? @n "{args.name}")) (struct_item name: (identifier) @n (#eq? @n "{args.name}")) (enum_item name: (identifier) @n (#eq? @n "{args.name}")) (impl_item) @scope] @scope',
        "c":          f'(function_definition declarator: (identifier) @n (#eq? @n "{args.name}")) @scope',
        "cpp":        f'[(function_definition declarator: (identifier) @n (#eq? @n "{args.name}")) (class_specifier name: (identifier) @n (#eq? @n "{args.name}"))] @scope',
    }.get(args.lang)

    if not q_str:
        print(json.dumps({"error": f"scope_extract not supported for lang '{args.lang}' yet"}))
        return

    try:
        q = language.query(q_str)
        caps = q.captures(tree.root_node)
        results = []
        for capture_name, nodes in caps.items():
            if capture_name == "scope":
                for node in nodes:
                    code = src[node.start_byte:node.end_byte].decode("utf-8", errors="replace")
                    results.append({
                        "file":       str(fpath),
                        "start_line": node.start_point[0] + 1,
                        "end_line":   node.end_point[0] + 1,
                        "code":       code[:3000],
                    })
        print(json.dumps({"name": args.name, "results": results}, ensure_ascii=False, indent=2))
    except Exception as e:
        print(json.dumps({"error": str(e)}))


# ─────────────────────────────────────────────────────────────────────────────
#  子命令：data_flow（函数级污点追踪）
# ─────────────────────────────────────────────────────────────────────────────

def _get_callers_with_context(target: str, lang: str, fn_name: str) -> list[dict]:
    """
    获取调用 fn_name 的所有调用者，包含调用者函数名（多语言支持）。
    支持：Python, JavaScript, TypeScript, Go, Java, Rust, C, C++

    实现策略：先用宽松的 call_site 匹配找到所有调用行，
    再在每个调用行附近向上寻找最近的函数定义作为 caller function name。
    这样比单一 query 更健壮，且对所有语言一视同仁。
    """
    import tree_sitter

    language = _load_language(lang)
    parser = tree_sitter.Parser(language)
    files = _collect_files(target, lang)

    results = []

    for fpath in files:
        tree, src = _parse_file(fpath, parser)
        if tree is None:
            continue

        # Step 1: 找到所有 fn_name 的调用点
        call_sites = _find_call_sites(tree, src, lang, fn_name)
        if not call_sites:
            continue

        # Step 2: 对每个调用点，向上搜索最近的函数定义
        for call_line in call_sites:
            caller_fn = _find_enclosing_function(tree, src, lang, call_line)
            if caller_fn:
                results.append({
                    "file": str(fpath),
                    "line": call_line,
                    "caller_function": caller_fn,
                })

    return results


def _find_call_sites(tree, src: bytes, lang: str, fn_name: str) -> list[int]:
    """
    查找 fn_name 的所有调用行号列表（兼容所有支持的语言）。
    """
    import tree_sitter

    try:
        call_site_q = {
            "python": """
(call
    function: [(identifier) @fn (attribute attribute: (identifier) @fn)]
)+ @call
""",
            "javascript": """
(call_expression
    function: [(identifier) @fn (member_expression property: (property_identifier) @fn)]
)+ @call
""",
            "typescript": """
(call_expression
    function: [(identifier) @fn
               (member_expression property: (property_identifier) @fn)]
)+ @call
""",
            "go": """
(call_expression
    function: [(identifier) @fn
               (selector_expression field: (field_identifier) @fn)]
)+ @call
""",
            "java": """
(method_invocation
    name: (identifier) @fn
)+ @call
""",
            "rust": """
(call_expression
    function: [(identifier) @fn
               (path_expression (path_identifier) @fn)]
)+ @call
""",
            "c": """
(call_expression
    function: (identifier) @fn
)+ @call
""",
            "cpp": """
(call_expression
    function: [(identifier) @fn
               (field_expression field: (field_identifier) @fn)]
)+ @call
""",
        }.get(lang)

        if not call_site_q:
            return []

        q = language.query(call_site_q)
        captures = q.captures(tree.root_node)

        call_lines = []
        for cap_name, nodes in captures.items():
            if cap_name == "fn":
                for node in nodes:
                    text_bytes = src[node.start_byte:node.end_byte].decode("utf-8", errors="replace")
                    if text_bytes == fn_name:
                        call_lines.append(node.start_point[0] + 1)
        return list(set(call_lines))

    except Exception:
        return []


def _find_enclosing_function(tree, src: bytes, lang: str, call_line: int) -> Optional[str]:
    """
    从 call_line 向上搜索，找到最近的函数定义的名称。
    返回函数名字符串；未找到返回 None。
    """
    import tree_sitter

    func_def_queries = {
        "python": """
(function_definition name: (identifier) @fn_name) @func
""",
        "javascript": """
[
  (function_declaration name: (identifier) @fn_name)
  (method_definition name: (property_identifier) @fn_name)
  (arrow_function) @arrow
]
""",
        "typescript": """
[
  (function_declaration name: (identifier) @fn_name)
  (method_definition name: (property_identifier) @fn_name)
  (arrow_function) @arrow
  (function_expression name: (identifier) @fn_name)
]
""",
        "go": """
[
  (function_declaration name: (identifier) @fn_name)
  (method_declaration name: (field_identifier) @fn_name)
]
""",
        "java": """
[
  (method_declaration name: (identifier) @fn_name)
  (constructor_declaration name: (identifier) @fn_name)
]
""",
        "rust": """
(function_item name: (identifier) @fn_name) @func
""",
        "c": """
(function_declaration declarator: (identifier) @fn_name) @func
""",
        "cpp": """
[
  (function_declarator declarator: (identifier) @fn_name) @func
  (function_definition declarator: (identifier) @fn_name) @func
  (method_declaration name: (field_identifier) @fn_name)
]
""",
    }

    q_str = func_def_queries.get(lang)
    if not q_str:
        return None

    try:
        q = language.query(q_str)
    except Exception:
        return None

    captures = q.captures(tree.root_node)
    best_fn = None
    best_distance = float("inf")

    for cap_name, nodes in captures.items():
        for node in nodes:
            func_start = node.start_point[0] + 1
            distance = call_line - func_start
            if 0 < distance < best_distance:
                fn_node = None
                if cap_name == "fn_name":
                    fn_node = node
                elif cap_name in ("func",):
                    fn_node = node.child_by_field_name("name")
                if fn_node:
                    name_text = src[fn_node.start_byte:fn_node.end_byte].decode("utf-8", errors="replace")
                    best_distance = distance
                    best_fn = name_text

    return best_fn



def _trace_backward(
    target: str, lang: str,
    current_fn: str, source_fn: str,
    current_path: list,
    depth: int, max_depth: int,
    visited: set,
) -> list[list]:
    """
    递归向上追踪：从 current_fn 寻找 source_fn。
    返回所有找到的路径（每条路径是函数名列表）。
    """
    if depth > max_depth:
        return []
    if current_fn == source_fn:
        return [current_path + [source_fn]]
    if current_fn in visited:
        return []  # 防止无限递归（环形调用）
    visited.add(current_fn)

    callers = _get_callers_with_context(target, lang, current_fn)
    if not callers:
        return []

    all_paths = []
    for caller in callers:
        caller_fn = caller.get("caller_function")
        if not caller_fn:
            continue
        new_path = current_path + [caller_fn]
        paths = _trace_backward(
            target, lang,
            caller_fn, source_fn,
            new_path,
            depth + 1, max_depth,
            visited,
        )
        all_paths.extend(paths)

    visited.discard(current_fn)
    return all_paths


def cmd_data_flow(args):
    """
    函数级数据流追踪：找出从 source 函数到 sink 函数的所有可达路径。
    基于调用图反向追踪，输出结构化路径供 LLM 进一步分析。
    """
    source_fn = args.source
    sink_fn = args.sink
    max_depth = args.max_depth or 5

    # 获取 sink 的所有直接调用者
    sink_callers = _get_callers_with_context(args.target, args.lang, sink_fn)
    if not sink_callers:
        print(json.dumps({
            "source": source_fn,
            "sink": sink_fn,
            "paths": [],
            "count": 0,
            "note": f"No callers found for sink function '{sink_fn}'",
        }, ensure_ascii=False, indent=2))
        return

    # 对每个调用者，递归向上追踪
    all_paths = []
    seen_paths = set()

    for caller in sink_callers:
        caller_fn = caller.get("caller_function")
        if not caller_fn:
            continue
        initial_path = [sink_fn, caller_fn]
        paths = _trace_backward(
            target=args.target,
            lang=args.lang,
            current_fn=caller_fn,
            source_fn=source_fn,
            current_path=initial_path,
            depth=1,
            max_depth=max_depth,
            visited=set(),
        )
        for p in paths:
            path_key = "->".join(p)
            if path_key not in seen_paths:
                seen_paths.add(path_key)
                all_paths.append(p)

    # 格式化输出：每条路径包含 file:line 信息
    formatted_paths = []
    for path in all_paths:
        formatted_paths.append({
            "path": " -> ".join(path),
            "length": len(path) - 1,
            "functions": path,
        })

    print(json.dumps({
        "source": source_fn,
        "sink": sink_fn,
        "max_depth": max_depth,
        "paths": formatted_paths[:50],  # 限制最多 50 条路径
        "count": len(formatted_paths),
        "truncated": len(all_paths) > 50,
        "note": (
            "Function-level taint tracking. For variable-level analysis, "
            "use the path results as input to LLM for further refinement."
        ),
    }, ensure_ascii=False, indent=2))


# ─────────────────────────────────────────────────────────────────────────────
#  CLI 入口
# ─────────────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(
        prog="semantic_helper",
        description="kimiSec semantic code analysis helper (AST/call-graph powered)",
    )
    sub = parser.add_subparsers(dest="command", required=True)

    # ast_query
    p1 = sub.add_parser("ast_query", help="Run Tree-sitter S-expression query over codebase")
    p1.add_argument("--target", "-t", required=True)
    p1.add_argument("--lang",   "-l", required=True, choices=list(LANG_MAP))
    p1.add_argument("--query",  "-q", required=True)
    p1.set_defaults(func=cmd_ast_query)

    # call_graph
    p2 = sub.add_parser("call_graph", help="Build call graph for a specific function")
    p2.add_argument("--target",    "-t", required=True)
    p2.add_argument("--lang",      "-l", required=True, choices=list(LANG_MAP))
    p2.add_argument("--function",  "-f", required=True)
    p2.add_argument("--direction", "-d", default="both",
                    choices=["callers", "callees", "both"])
    p2.set_defaults(func=cmd_call_graph)

    # scope_extract
    p3 = sub.add_parser("scope_extract", help="Extract full AST scope of a function or class")
    p3.add_argument("--target", "-t", required=True)
    p3.add_argument("--lang",   "-l", required=True, choices=list(LANG_MAP))
    p3.add_argument("--name",   "-n", required=True)
    p3.set_defaults(func=cmd_scope_extract)

    # data_flow
    p4 = sub.add_parser("data_flow", help="Trace data flow from source function to sink")
    p4.add_argument("--target", "-t", required=True)
    p4.add_argument("--lang",   "-l", required=True, choices=list(LANG_MAP))
    p4.add_argument("--source", "-s", required=True, help="Source function name (entry point)")
    p4.add_argument("--sink",   "-k", required=True, help="Sink function name (dangerous sink)")
    p4.add_argument("--max-depth", "-d", type=int, default=5,
                    help="Maximum recursion depth for traceback (default: 5)")
    p4.set_defaults(func=cmd_data_flow)

    args = parser.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
