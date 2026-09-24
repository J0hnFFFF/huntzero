"""
SemanticHelper — CPG 轻量导航器
=================================
基于 tree-sitter 的多语言代码结构提取引擎。
为 Drone 提供 AST 级别的精准代码导航，替代基于文本的 grep 查询。

暴露 4 个核心工具（设计为 MCP Tool 接口）：
  1. list_functions(file)           — 提取文件中所有函数/方法的签名和位置
  2. find_references(symbol, dir)   — 在目录中找 symbol 的所有引用点（AST 级过滤，排除注释和字符串）
  3. get_call_graph(file)           — 提取文件内的函数调用关系图
  4. get_function_body(file, name)  — 精确提取函数完整源码（不多一行，不少一行）

设计原则：
  - 工具只回答事实性问题（"谁调用了 X？"），不做安全判断
  - 安全判断由 LLM Drone 基于第一性原理来做
  - 优雅降级：tree-sitter 解析失败时回退到正则/文本方法，不崩溃
"""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path
from typing import Optional


# ─── 语言映射 ────────────────────────────────────────────────────────────────

_LANG_MAP: dict[str, str] = {
    ".py":   "python",
    ".js":   "javascript",
    ".jsx":  "javascript",
    ".ts":   "typescript",
    ".tsx":  "typescript",
    ".go":   "go",
    ".c":    "c",
    ".h":    "c",
    ".cc":   "cpp",
    ".cpp":  "cpp",
    ".cxx":  "cpp",
    ".hpp":  "cpp",
    ".java": "java",
    ".rs":   "rust",
    ".rb":   "ruby",
    ".kt":   "kotlin",
    ".swift":"swift",
}

# tree-sitter 函数定义节点类型（跨语言统一）
_FUNC_NODE_TYPES: dict[str, list[str]] = {
    "python":     ["function_definition", "async_function_definition"],
    "javascript": ["function_declaration", "function_expression",
                   "arrow_function", "method_definition"],
    "typescript": ["function_declaration", "function_expression",
                   "arrow_function", "method_definition",
                   "method_signature", "function_signature"],
    "go":         ["function_declaration", "method_declaration"],
    "c":          ["function_definition"],
    "cpp":        ["function_definition", "template_function"],
    "java":       ["method_declaration", "constructor_declaration"],
    "rust":       ["function_item"],
}

# tree-sitter 调用表达式节点类型
_CALL_NODE_TYPES: dict[str, list[str]] = {
    "python":     ["call"],
    "javascript": ["call_expression", "new_expression"],
    "typescript": ["call_expression", "new_expression"],
    "go":         ["call_expression"],
    "c":          ["call_expression"],
    "cpp":        ["call_expression"],
    "java":       ["method_invocation", "object_creation_expression"],
    "rust":       ["call_expression", "method_call_expression"],
}

# ─── 语言解析器懒加载缓存 ────────────────────────────────────────────────────

_parser_cache: dict[str, object] = {}   # lang_name → tree_sitter.Parser
_lang_cache: dict[str, object] = {}     # lang_name → tree_sitter.Language


def _get_parser(lang: str):
    """获取指定语言的 tree-sitter Parser（懒加载 + 缓存）。"""
    if lang in _parser_cache:
        return _parser_cache[lang]
    try:
        import tree_sitter
        if not hasattr(tree_sitter, "Language"):
            raise ImportError("tree-sitter API mismatch")

        lang_module_map = {
            "python":     "tree_sitter_python",
            "javascript": "tree_sitter_javascript",
            "typescript": "tree_sitter_typescript",
            "go":         "tree_sitter_go",
            "c":          "tree_sitter_c",
            "cpp":        "tree_sitter_cpp",
            "java":       "tree_sitter_java",
            "rust":       "tree_sitter_rust",
        }
        module_name = lang_module_map.get(lang)
        if not module_name:
            return None

        import importlib
        lang_mod = importlib.import_module(module_name)
        language = tree_sitter.Language(lang_mod.language())
        parser = tree_sitter.Parser(language)
        _parser_cache[lang] = parser
        _lang_cache[lang] = language
        return parser
    except (ImportError, Exception):
        _parser_cache[lang] = None
        return None


def _detect_lang(file_path: Path) -> Optional[str]:
    """从文件扩展名推断 tree-sitter 语言名称。"""
    return _LANG_MAP.get(file_path.suffix.lower())


def _parse_file(file_path: Path) -> tuple[object, bytes, str, str]:
    """
    解析文件，返回 (tree, source_bytes, source_str, lang)。
    若解析失败返回 (None, b"", "", lang)。
    """
    try:
        source_str = file_path.read_text(encoding="utf-8", errors="ignore")
        source_bytes = source_str.encode("utf-8")
    except OSError:
        return None, b"", "", ""

    lang = _detect_lang(file_path)
    if not lang:
        return None, source_bytes, source_str, ""

    parser = _get_parser(lang)
    if not parser:
        return None, source_bytes, source_str, lang

    try:
        tree = parser.parse(source_bytes)
        return tree, source_bytes, source_str, lang
    except Exception:
        return None, source_bytes, source_str, lang


def _iter_nodes(node, type_filter: Optional[set[str]] = None):
    """递归遍历 AST 节点，可按节点类型过滤。"""
    if type_filter is None or node.type in type_filter:
        yield node
    for child in node.children:
        yield from _iter_nodes(child, type_filter)


def _get_node_name(node, source_bytes: bytes) -> Optional[str]:
    """
    从函数/方法节点中提取名称。
    不同语言的"name 子节点"位置不同，统一处理。
    """
    for child in node.children:
        if child.type in ("identifier", "name", "property_identifier",
                          "field_identifier", "type_identifier"):
            return source_bytes[child.start_byte:child.end_byte].decode("utf-8", errors="ignore")
    return None


def _get_call_target(node, source_bytes: bytes) -> Optional[str]:
    """
    从调用表达式节点中提取被调用的函数名（尽力而为）。
    返回最终的 identifier 部分（如 `pkg.Func()` → `Func`，`obj.method()` → `method`）。
    """
    # 遍历第一个子节点（通常是函数引用部分）
    if not node.children:
        return None
    func_part = node.children[0]

    raw = source_bytes[func_part.start_byte:func_part.end_byte].decode("utf-8", errors="ignore").strip()
    # 取最后一段（处理链式调用 a.b.c → c）
    parts = re.split(r'[.\->\:]+', raw)
    name = parts[-1].strip() if parts else None
    # 过滤掉纯空白和括号
    return name if name and re.match(r'^[A-Za-z_]\w*$', name) else None


# ─── 公开工具函数 ─────────────────────────────────────────────────────────────

def list_functions(file_path: str) -> list[dict]:
    """
    提取文件中所有函数/方法的签名信息。

    返回列表，每项包含：
      - name: 函数名
      - line: 起始行号（1-indexed）
      - end_line: 结束行号
      - signature: 函数签名（第一行）
      - lang: 语言
    """
    path = Path(file_path)
    if not path.exists():
        return [{"error": f"File not found: {file_path}"}]

    tree, source_bytes, source_str, lang = _parse_file(path)
    lines = source_str.splitlines()
    results = []

    if tree:
        func_types = set(_FUNC_NODE_TYPES.get(lang, []))
        for node in _iter_nodes(tree.root_node, func_types):
            name = _get_node_name(node, source_bytes)
            start_line = node.start_point[0] + 1
            end_line = node.end_point[0] + 1
            # 签名 = 函数定义的第一行（截断到合理长度）
            sig_line = lines[start_line - 1].strip() if start_line <= len(lines) else ""
            results.append({
                "name": name or "<anonymous>",
                "line": start_line,
                "end_line": end_line,
                "signature": sig_line[:200],
                "lang": lang,
            })
    else:
        # 降级回退：正则提取函数名
        results = _fallback_list_functions(source_str, lang)

    return results


def find_references(
    symbol: str,
    search_dir: str,
    extensions: Optional[list[str]] = None,
    max_results: int = 50,
) -> list[dict]:
    """
    在目录中查找 symbol 的所有引用（AST 级别，不包含注释和字符串内的出现）。

    参数：
      symbol      — 要查找的函数名/变量名/类名
      search_dir  — 搜索目录
      extensions  — 限制文件扩展名（如 [".go", ".py"]），None 表示全部
      max_results — 最多返回多少条结果

    每条结果包含：
      - file: 相对路径
      - line: 行号（1-indexed）
      - col: 列号
      - context: 引用所在行的代码（带前后各 1 行）
      - node_type: 节点类型（call_expression / identifier 等）
    """
    root = Path(search_dir)
    if not root.exists():
        return [{"error": f"Directory not found: {search_dir}"}]

    results = []
    exts = set(extensions) if extensions else set(_LANG_MAP.keys())

    for file_path in sorted(root.rglob("*")):
        if len(results) >= max_results:
            break
        if not file_path.is_file():
            continue
        if file_path.suffix.lower() not in exts:
            continue
        # 跳过常见的噪音目录
        if any(part.startswith(".") or part in ("vendor", "node_modules",
                "__pycache__", "testdata", "third_party")
               for part in file_path.parts):
            continue

        tree, source_bytes, source_str, lang = _parse_file(file_path)
        lines = source_str.splitlines()
        rel_path = str(file_path.relative_to(root))

        if tree:
            refs = _find_ast_references(tree, source_bytes, symbol, lang)
        else:
            refs = _fallback_find_references(source_str, symbol)

        for ref in refs:
            line_idx = ref["line"] - 1
            context_lines = lines[max(0, line_idx - 1): line_idx + 2]
            results.append({
                "file": rel_path,
                "line": ref["line"],
                "col": ref.get("col", 0),
                "context": "\n".join(context_lines),
                "node_type": ref.get("node_type", "identifier"),
            })
            if len(results) >= max_results:
                break

    return results


def get_call_graph(file_path: str) -> dict:
    """
    提取文件内的函数调用关系图。

    返回值：
      {
        "functions": {
          "funcA": {
            "calls": ["funcB", "funcC"],
            "called_by": []    # 本文件范围内
          },
          ...
        },
        "external_calls": ["os.Open", "http.Get"]   # 外部调用（文件外或标准库）
      }
    """
    path = Path(file_path)
    if not path.exists():
        return {"error": f"File not found: {file_path}"}

    tree, source_bytes, source_str, lang = _parse_file(path)
    if not tree:
        return {"error": f"Could not parse: {file_path}", "lang": lang}

    func_types = set(_FUNC_NODE_TYPES.get(lang, []))
    call_types = set(_CALL_NODE_TYPES.get(lang, []))

    # 先收集所有函数节点
    func_nodes: list[tuple[str, object]] = []
    for node in _iter_nodes(tree.root_node, func_types):
        name = _get_node_name(node, source_bytes) or "<anonymous>"
        func_nodes.append((name, node))

    local_func_names = {name for name, _ in func_nodes}

    # 对每个函数，收集其调用的函数
    graph: dict[str, dict] = {}
    all_external: set[str] = set()

    for func_name, func_node in func_nodes:
        calls_local: list[str] = []
        calls_external: list[str] = []

        for call_node in _iter_nodes(func_node, call_types):
            target = _get_call_target(call_node, source_bytes)
            if not target:
                continue
            if target in local_func_names and target != func_name:
                calls_local.append(target)
            elif target and target not in ("if", "for", "while", "return"):
                calls_external.append(target)

        # 去重保序
        calls_local = list(dict.fromkeys(calls_local))
        calls_external = list(dict.fromkeys(calls_external))
        all_external.update(calls_external)

        graph[func_name] = {
            "calls": calls_local,
            "external_calls": calls_external[:20],  # 适当截断外部调用噪音
        }

    # 填充 called_by（反向图，仅限文件内部）
    for func_name in graph:
        graph[func_name]["called_by"] = [
            caller for caller, data in graph.items()
            if func_name in data["calls"]
        ]

    return {
        "file": file_path,
        "lang": lang,
        "functions": graph,
        "external_calls": sorted(all_external)[:50],
    }


def get_function_body(file_path: str, func_name: str) -> dict:
    """
    精确提取某个函数的完整源代码，不多一行，不少一行。

    返回：
      {
        "name": func_name,
        "file": file_path,
        "start_line": N,
        "end_line": M,
        "source": "完整函数源码",
        "lang": "go"
      }
    若文件中有多个同名函数（重载），返回第一个匹配。
    """
    path = Path(file_path)
    if not path.exists():
        return {"error": f"File not found: {file_path}"}

    tree, source_bytes, source_str, lang = _parse_file(path)
    lines = source_str.splitlines()

    if not tree:
        # 降级：正则搜索
        return _fallback_get_function_body(source_str, func_name, file_path, lang)

    func_types = set(_FUNC_NODE_TYPES.get(lang, []))
    for node in _iter_nodes(tree.root_node, func_types):
        name = _get_node_name(node, source_bytes)
        if name != func_name:
            continue
        start_line = node.start_point[0] + 1
        end_line = node.end_point[0] + 1
        body_lines = lines[start_line - 1: end_line]
        return {
            "name": func_name,
            "file": file_path,
            "start_line": start_line,
            "end_line": end_line,
            "source": "\n".join(body_lines),
            "lang": lang,
        }

    return {"error": f"Function '{func_name}' not found in {file_path}"}


# ─── 内部辅助函数 ─────────────────────────────────────────────────────────────

def _find_ast_references(tree, source_bytes: bytes, symbol: str, lang: str) -> list[dict]:
    """
    在 AST 树中查找 symbol 的所有引用，自动排除注释和字符串字面量中的出现。
    """
    # 需要排除的"噪音"节点类型（注释和字符串）
    skip_types = {
        "comment", "line_comment", "block_comment", "doc_comment",
        "string", "string_literal", "interpreted_string_literal",
        "raw_string_literal", "template_string",
    }
    refs = []

    def _is_inside_skip_node(node) -> bool:
        parent = node.parent
        while parent:
            if parent.type in skip_types:
                return True
            parent = parent.parent
        return False

    for node in _iter_nodes(tree.root_node):
        if node.type not in ("identifier", "type_identifier",
                             "field_identifier", "property_identifier"):
            continue
        name = source_bytes[node.start_byte:node.end_byte].decode("utf-8", errors="ignore")
        if name != symbol:
            continue
        if _is_inside_skip_node(node):
            continue
        # 判断这是不是一个函数调用
        parent_type = node.parent.type if node.parent else "unknown"
        node_type = "call_expression" if "call" in parent_type else "identifier"
        refs.append({
            "line": node.start_point[0] + 1,
            "col": node.start_point[1],
            "node_type": node_type,
        })

    return refs


# ─── 降级回退实现（不依赖 tree-sitter）────────────────────────────────────────

_FALLBACK_FUNC_PATTERNS: dict[str, list[str]] = {
    "python": [
        r'^(?:async\s+)?def\s+(\w+)\s*\(',
    ],
    "go": [
        r'^func\s+(?:\(\w+\s+\*?\w+\)\s+)?(\w+)\s*\(',
    ],
    "javascript": [
        r'^(?:async\s+)?function\s+(\w+)\s*\(',
        r'^\s*(\w+)\s*[:=]\s*(?:async\s+)?(?:function|\()',
        r'^\s*(?:async\s+)?(\w+)\s*\(',
    ],
    "c": [
        r'^\w[\w\s\*]+\s+(\w+)\s*\(',
    ],
    "cpp": [
        r'^\w[\w\s\*\:<>]+\s+(\w+)\s*\(',
    ],
    "java": [
        r'(?:public|private|protected|static|final|\s)+[\w<>\[\]]+\s+(\w+)\s*\(',
    ],
}


def _fallback_list_functions(source: str, lang: str) -> list[dict]:
    patterns = _FALLBACK_FUNC_PATTERNS.get(lang, [r'^(?:def|func|function)\s+(\w+)'])
    results = []
    for i, line in enumerate(source.splitlines(), 1):
        for pat in patterns:
            m = re.match(pat, line.strip())
            if m:
                results.append({
                    "name": m.group(1),
                    "line": i,
                    "end_line": i,
                    "signature": line.strip()[:200],
                    "lang": lang,
                    "method": "fallback_regex",
                })
                break
    return results


def _fallback_find_references(source: str, symbol: str) -> list[dict]:
    """纯文本搜索，返回独立单词匹配（非子串）。"""
    pattern = re.compile(r'\b' + re.escape(symbol) + r'\b')
    results = []
    for i, line in enumerate(source.splitlines(), 1):
        if pattern.search(line):
            results.append({"line": i, "col": 0, "node_type": "text_match"})
    return results


def _fallback_get_function_body(
    source: str, func_name: str, file_path: str, lang: str
) -> dict:
    """降级：用正则和括号匹配提取函数体。"""
    lines = source.splitlines()
    for i, line in enumerate(lines, 1):
        if re.search(r'\b' + re.escape(func_name) + r'\s*\(', line):
            # 找到函数定义起始行，往下找到匹配的闭括号
            depth = 0
            started = False
            for j in range(i - 1, len(lines)):
                for ch in lines[j]:
                    if ch == "{":
                        depth += 1
                        started = True
                    elif ch == "}":
                        depth -= 1
                if started and depth <= 0:
                    return {
                        "name": func_name,
                        "file": file_path,
                        "start_line": i,
                        "end_line": j + 1,
                        "source": "\n".join(lines[i - 1: j + 1]),
                        "lang": lang,
                        "method": "fallback_bracket_match",
                    }
    return {"error": f"Function '{func_name}' not found via fallback in {file_path}"}


# ─────────────────────────────────────────────────────────────────────────────
#  分析子命令：ast_query / call_graph / scope_extract / data_flow
#  —— 合并自 tools/semantic_helper.py（SPEC 5.1）
#  tree-sitter 0.26 兼容性：经 _ts_query/_ts_captures 封装（见下）。
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


def _ts_query(language, query_str: str):
    """
    tree-sitter 查询兼容封装（合并时新增）。

    tree-sitter ≥ 0.25 移除了 Language.query()，改用
    tree_sitter.Query(language, source)；旧 API 存在时仍走旧路径。
    """
    import tree_sitter
    if hasattr(language, "query"):
        return language.query(query_str)
    return tree_sitter.Query(language, query_str)


def _ts_captures(query, node) -> dict:
    """
    tree-sitter captures 兼容封装（合并时新增）。

    tree-sitter ≥ 0.25 移除了 Query.captures()，改用
    tree_sitter.QueryCursor(query).captures(node)；旧 API 存在时仍走旧路径。
    """
    import tree_sitter
    if hasattr(query, "captures"):
        return query.captures(node)
    return tree_sitter.QueryCursor(query).captures(node)


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


def _parse_source(path: Path, parser) -> Optional[object]:
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
        query = _ts_query(language, args.query)
    except Exception as e:
        print(json.dumps({"error": f"Invalid query: {e}"}))
        sys.exit(1)

    results = []
    for fpath in files:
        tree, src = _parse_source(fpath, parser)
        if tree is None:
            continue
        captures = _ts_captures(query, tree.root_node)
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

    callers, definitions = [], []
    call_q_str = call_site_queries.get(args.lang)
    def_q_str  = def_queries.get(args.lang)

    for fpath in files:
        tree, src = _parse_source(fpath, parser)
        if tree is None:
            continue

        if call_q_str and args.direction in ("callers", "both"):
            try:
                q = _ts_query(language, call_q_str)
                caps = _ts_captures(q, tree.root_node)
                for _, nodes in caps.items():
                    for node in nodes:
                        text = src[node.start_byte:node.end_byte].decode("utf-8", errors="replace")
                        callers.append({"file": str(fpath), "line": node.start_point[0]+1, "text": text[:200]})
            except Exception:
                pass

        if def_q_str:
            try:
                q = _ts_query(language, def_q_str)
                caps = _ts_captures(q, tree.root_node)
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

    tree, src = _parse_source(fpath, parser)
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
        q = _ts_query(language, q_str)
        caps = _ts_captures(q, tree.root_node)
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
        tree, src = _parse_source(fpath, parser)
        if tree is None:
            continue

        # Step 1: 找到所有 fn_name 的调用点
        call_sites = _find_call_sites(tree, src, language, lang, fn_name)
        if not call_sites:
            continue

        # Step 2: 对每个调用点，向上搜索最近的函数定义
        for call_line in call_sites:
            caller_fn = _find_enclosing_function(tree, src, language, lang, call_line)
            if caller_fn:
                results.append({
                    "file": str(fpath),
                    "line": call_line,
                    "caller_function": caller_fn,
                })

    return results


def _find_call_sites(tree, src: bytes, language, lang: str, fn_name: str) -> list[int]:
    """
    查找 fn_name 的所有调用行号列表（兼容所有支持的语言）。

    合并修复：language 改为显式形参——原版引用未定义的同名变量，
    NameError 被 except 静默吞掉导致恒返回空列表。
    """
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

        q = _ts_query(language, call_site_q)
        captures = _ts_captures(q, tree.root_node)

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


def _find_enclosing_function(tree, src: bytes, language, lang: str, call_line: int) -> Optional[str]:
    """
    从 call_line 向上搜索，找到最近的函数定义的名称。
    返回函数名字符串；未找到返回 None。

    合并修复：language 改为显式形参（同 _find_call_sites）。
    """
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
        q = _ts_query(language, q_str)
    except Exception:
        return None

    captures = _ts_captures(q, tree.root_node)
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
        # current_path 恒以 source_fn 结尾（调用方构造不变式），直接返回；
        # 原版 current_path + [source_fn] 会导致路径尾部重复。
        return [current_path]
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


def _run_analysis_command(cmd: str, argv: list[str]) -> None:
    """
    分析子命令 CLI 适配层（合并自 tools/semantic_helper.py）。

    保留本文件的手写分发结构；新并入的 4 个分析子命令沿用
    tools 版 argparse 选项接口（-t/-l/-q/-f/-n/-s/-k/-d 逐字保留）。
    """
    import argparse

    parser = argparse.ArgumentParser(prog=f"semantic_helper.py {cmd}")

    if cmd == "ast_query":
        parser.add_argument("--target", "-t", required=True)
        parser.add_argument("--lang",   "-l", required=True, choices=list(LANG_MAP))
        parser.add_argument("--query",  "-q", required=True)
        parser.set_defaults(func=cmd_ast_query)
    elif cmd == "call_graph":
        parser.add_argument("--target",    "-t", required=True)
        parser.add_argument("--lang",      "-l", required=True, choices=list(LANG_MAP))
        parser.add_argument("--function",  "-f", required=True)
        parser.add_argument("--direction", "-d", default="both",
                            choices=["callers", "both"])
        parser.set_defaults(func=cmd_call_graph)
    elif cmd == "scope_extract":
        parser.add_argument("--target", "-t", required=True)
        parser.add_argument("--lang",   "-l", required=True, choices=list(LANG_MAP))
        parser.add_argument("--name",   "-n", required=True)
        parser.set_defaults(func=cmd_scope_extract)
    elif cmd == "data_flow":
        parser.add_argument("--target", "-t", required=True)
        parser.add_argument("--lang",   "-l", required=True, choices=list(LANG_MAP))
        parser.add_argument("--source", "-s", required=True, help="Source function name (entry point)")
        parser.add_argument("--sink",   "-k", required=True, help="Sink function name (dangerous sink)")
        parser.add_argument("--max-depth", "-d", type=int, default=5,
                            help="Maximum recursion depth for traceback (default: 5)")
        parser.set_defaults(func=cmd_data_flow)
    else:
        print(f"Unknown command: {cmd}")
        sys.exit(1)

    args = parser.parse_args(argv)
    args.func(args)


# ─── CLI 自测接口 ─────────────────────────────────────────────────────────────

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
