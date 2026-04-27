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

import re
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


# ─── CLI 自测接口 ─────────────────────────────────────────────────────────────

if __name__ == "__main__":
    import json
    import sys

    if len(sys.argv) < 3:
        print("Usage: python semantic_helper.py <command> <args...>")
        print("Commands:")
        print("  list_functions <file>")
        print("  find_references <symbol> <directory>")
        print("  get_call_graph <file>")
        print("  get_function_body <file> <func_name>")
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
    else:
        print(f"Unknown command: {cmd}")
        sys.exit(1)

    print(json.dumps(result, indent=2, ensure_ascii=False))
