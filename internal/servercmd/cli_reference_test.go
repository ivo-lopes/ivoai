package servercmd

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

func TestServerCanonicalHelpCoversPublicCommandStructure(t *testing.T) {
	var output bytes.Buffer
	(&runner{out: &output}).usage()
	help := output.String()
	_, source, _, _ := runtime.Caller(0)
	parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(filepath.Dir(source), "runner.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	functions := map[string]bool{
		"run": true, "enrollment": true, "webAccess": true, "connector": true,
		"context": true, "memory": true, "docs": true, "gateway": true, "remote": true,
	}
	commands := map[string]bool{}
	flags := map[string]bool{}
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
				if !ok || (selector.Sel.Name != "String" && selector.Sel.Name != "Bool" && selector.Sel.Name != "Int" && selector.Sel.Name != "Duration") || len(value.Args) == 0 {
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
	for command := range commands {
		if !regexp.MustCompile(`(^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(command) + `([^A-Za-z0-9_-]|$)`).MatchString(help) {
			t.Errorf("public server command or subcommand %q is missing from canonical help", command)
		}
	}
	for flag := range flags {
		if !strings.Contains(help, "--"+flag) {
			t.Errorf("public server flag --%s is missing from canonical help", flag)
		}
	}
}
