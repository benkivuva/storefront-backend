package arch

import (
"go/parser"
"go/token"
"os"
"path/filepath"
"strconv"
"strings"
"testing"
)

const modulePath = "github.com/benkivuva/storefront-backend"

func repoRoot(t *testing.T) string {
t.Helper()
root, err := filepath.Abs(filepath.Join("..", ".."))
if err != nil {
t.Fatalf("resolve repo root: %v", err)
}
return root
}

// importsOf parses non-test *.go files under dir and returns their imports.
// A missing directory is not a violation — the guard activates once the
// package exists (e.g. internal/service is created in Sprint 2).
func importsOf(t *testing.T, dir string) []string {
t.Helper()
if _, err := os.Stat(dir); err != nil {
if os.IsNotExist(err) {
return nil
}
t.Fatalf("stat %s: %v", dir, err)
}
var imports []string
fset := token.NewFileSet()
err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
if walkErr != nil {
return walkErr
}
if d.IsDir() {
return nil
}
name := d.Name()
if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
return nil
}
f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
if err != nil {
return err
}
for _, imp := range f.Imports {
p, err := strconv.Unquote(imp.Path.Value)
if err != nil {
return err
}
imports = append(imports, p)
}
return nil
})
if err != nil {
t.Fatalf("walk %s: %v", dir, err)
}
return imports
}

func isStdlib(importPath string) bool {
first := strings.SplitN(importPath, "/", 2)[0]
return !strings.Contains(first, ".")
}

func TestDomainImportsStdlibOnly(t *testing.T) {
root := repoRoot(t)
for _, imp := range importsOf(t, filepath.Join(root, "internal", "domain")) {
if !isStdlib(imp) {
t.Errorf("internal/domain must import the standard library only; found %q", imp)
}
}
}

func TestServiceDoesNotImportAdapter(t *testing.T) {
root := repoRoot(t)
prefix := modulePath + "/internal/adapter"
for _, imp := range importsOf(t, filepath.Join(root, "internal", "service")) {
if strings.HasPrefix(imp, prefix) {
t.Errorf("internal/service must not import adapters; found %q", imp)
}
}
}
