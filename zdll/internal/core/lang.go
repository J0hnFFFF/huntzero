package core

// LangExtensions maps file extensions to language names.
var LangExtensions = map[string]string{
	".py":    "python",
	".js":    "javascript",
	".jsx":   "javascript",
	".ts":    "typescript",
	".tsx":   "typescript",
	".go":    "go",
	".c":     "c",
	".h":     "c",
	".cc":    "cpp",
	".cpp":   "cpp",
	".cxx":   "cpp",
	".hpp":   "cpp",
	".java":  "java",
	".rs":    "rust",
	".rb":    "ruby",
	".kt":    "kotlin",
	".swift": "swift",
}

// FuncNodeTypes maps tree-sitter function definition node types per language.
var FuncNodeTypes = map[string][]string{
	"python":     {"function_definition", "async_function_definition"},
	"javascript": {"function_declaration", "function_expression", "arrow_function", "method_definition"},
	"typescript": {"function_declaration", "function_expression", "arrow_function", "method_definition", "method_signature", "function_signature"},
	"go":         {"function_declaration", "method_declaration"},
	"c":          {"function_definition"},
	"cpp":        {"function_definition", "template_function"},
	"java":       {"method_declaration", "constructor_declaration"},
	"rust":       {"function_item"},
}

// CallNodeTypes maps tree-sitter call expression node types per language.
var CallNodeTypes = map[string][]string{
	"python":     {"call"},
	"javascript": {"call_expression", "new_expression"},
	"typescript": {"call_expression", "new_expression"},
	"go":         {"call_expression"},
	"c":          {"call_expression"},
	"cpp":        {"call_expression"},
	"java":       {"method_invocation", "object_creation_expression"},
	"rust":       {"call_expression", "method_call_expression"},
}

// FallbackFuncPatterns provides regex patterns for extracting function names
// when tree-sitter is unavailable.
var FallbackFuncPatterns = map[string][]string{
	"python": {
		`^(?:async\s+)?def\s+(\w+)\s*\(`,
	},
	"go": {
		`^func\s+(?:\(\w+\s+\*?\w+\)\s+)?(\w+)\s*\(`,
	},
	"javascript": {
		`^(?:async\s+)?function\s+(\w+)\s*\(`,
		`^\s*(\w+)\s*[:=]\s*(?:async\s+)?(?:function|\()`,
		`^\s*(?:async\s+)?(\w+)\s*\(`,
	},
	"c": {
		`^\w[\w\s\*]+\s+(\w+)\s*\(`,
	},
	"cpp": {
		`^\w[\w\s\*\:<>]+\s+(\w+)\s*\(`,
	},
	"java": {
		`(?:public|private|protected|static|final|\s)+[\w<>\[\]]+\s+(\w+)\s*\(`,
	},
}
