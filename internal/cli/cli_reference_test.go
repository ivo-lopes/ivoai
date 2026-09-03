package cli

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// TestCanonicalHelpCoversPublicCommandStructure prevents the generated CLI
// reference from silently drifting when a public switch branch or flag is
// added. The Markdown generator consumes this same canonical help output.
func TestCanonicalHelpCoversPublicCommandStructure(t *testing.T) {
	var output bytes.Buffer
	usage(&output)
	help := output.String()
	_, source, _, _ := runtime.Caller(0)
	files := []string{filepath.Join(filepath.Dir(source), "cli.go"), filepath.Join(filepath.Dir(source), "monitor.go")}
	functions := map[string]bool{
		"runCommand": true, "runSession": true, "runConnect": true,
		"runDisconnect": true, "runMemory": true, "runConfig": true,
		"runProject": true, "runMonitor": true,
	}
	assertSourceSurfaceDocumented(t, files, functions, help)
}

func assertSourceSurfaceDocumented(t *testing.T, files []string, functions map[string]bool, help string) {
	t.Helper()
	commands := map[string]bool{}
	flags := map[string]bool{}
	for _, filename := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || !functions[function.Name.Name] || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				switch value := node.(type) {
				case *ast.CaseClause:
					for _, expression := range value.List {
						literal, ok := expression.(*ast.BasicLit)
						if !ok || literal.Kind != token.STRING {
							continue
						}
						name, err := strconv.Unquote(literal.Value)
						if err == nil && name != "" && !strings.HasPrefix(name, "_") && !strings.HasPrefix(name, "-") {
							commands[name] = true
						}
					}
				case *ast.CallExpr:
					selector, ok := value.Fun.(*ast.SelectorExpr)
					if !ok || (selector.Sel.Name != "String" && selector.Sel.Name != "Bool" && selector.Sel.Name != "Int") || len(value.Args) == 0 {
						return true
					}
					literal, ok := value.Args[0].(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						return true
					}
					name, err := strconv.Unquote(literal.Value)
					if err == nil && name != "" {
						flags[name] = true
					}
				}
				return true
			})
		}
	}
	for command := range commands {
		if !regexp.MustCompile(`(^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(command) + `([^A-Za-z0-9_-]|$)`).MatchString(help) {
			t.Errorf("public command or subcommand %q is missing from canonical help", command)
		}
	}
	for flag := range flags {
		if !strings.Contains(help, "--"+flag) {
			t.Errorf("public flag --%s is missing from canonical help", flag)
		}
	}
}
