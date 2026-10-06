package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestLLMUsageMuAlwaysDeferredUnlock checks that every llmUsageMu.Lock() in
// the package is followed directly by `defer llmUsageMu.Unlock()`. A plain
// Unlock at the end of a block is skipped if the block panics. The Store
// recovers handler panics (dispatchWithPanicGuard), so a held mutex would
// then hang every later llm.usage.* command.
func TestLLMUsageMuAlwaysDeferredUnlock(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	locks := 0
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				block, ok := n.(*ast.BlockStmt)
				if !ok {
					return true
				}
				for i, stmt := range block.List {
					if !isMuCall(stmt, "Lock") {
						continue
					}
					locks++
					if i+1 >= len(block.List) || !isDeferredMuUnlock(block.List[i+1]) {
						t.Errorf("%s: llmUsageMu.Lock() not followed by defer llmUsageMu.Unlock()", fset.Position(stmt.Pos()))
					}
				}
				return true
			})
		}
	}
	if locks == 0 {
		t.Fatal("found no llmUsageMu.Lock() calls; the check is not looking at the right code")
	}
}

func isMuSelector(e ast.Expr, method string) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != method {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "llmUsageMu"
}

func isMuCall(stmt ast.Stmt, method string) bool {
	es, ok := stmt.(*ast.ExprStmt)
	return ok && isMuSelector(es.X, method)
}

func isDeferredMuUnlock(stmt ast.Stmt) bool {
	ds, ok := stmt.(*ast.DeferStmt)
	return ok && isMuSelector(ds.Call, "Unlock")
}
