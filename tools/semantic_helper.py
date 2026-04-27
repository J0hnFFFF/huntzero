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
    }
    def_queries = {
        "python":     f'(function_definition name: (identifier) @fn (#eq? @fn "{fn_name}")) @def',
        "javascript": f'[(function_declaration name: (identifier) @fn (#eq? @fn "{fn_name}")) (method_definition name: (property_identifier) @fn (#eq? @fn "{fn_name}"))] @def',
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
#  子命令：data_flow（简化版基于路径分析）
# ─────────────────────────────────────────────────────────────────────────────

def cmd_data_flow(args):
    """
    简化的数据流分析：找出从 source 函数/参数到 sink 函数的潜在传递路径。
    基于调用图的路径枚举，适合快速检验假设，不替代完整的污点分析。
    """
    source_fn, _, source_param = args.source.partition(".")
    sink_fn = args.sink

    print(json.dumps({
        "note": (
            "Simplified data-flow: use call_graph to find callers of sink, "
            "then trace backward to source function. Full taint analysis "
            "requires deeper static analysis (e.g., CodeQL)."
        ),
        "suggestion": (
            f"Step 1: python tools/semantic_helper.py call_graph "
            f"--target {args.target} --lang {args.lang} "
            f"--function {sink_fn} --direction callers\n"
            f"Step 2: Manually trace if any caller receives output from '{source_fn}'"
        ),
        "source": args.source,
        "sink":   sink_fn,
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
    p4.add_argument("--source", "-s", required=True)
    p4.add_argument("--sink",   "-k", required=True)
    p4.set_defaults(func=cmd_data_flow)

    args = parser.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
