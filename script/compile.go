package script

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// CompileExpr with panic on error.
//func (w *World) MustCompileExpr(Expr string) Expr {
//	tree, e := parser.ParseExpr(Expr)
//	if e != nil {
//		panic(err(fmt.Sprint(e)))
//	}
//	if w.Debug {
//		ast.Print(nil, tree)
//	}
//	return w.compileExpr(tree)
//}
//
//// Compiles an expression, which can then be evaluated. E.g.:
//// 	expr, err := world.CompileExpr("1+1")
//// 	expr.Eval()   // returns 2
//func (w *World) CompileExpr(src string) (code Expr, e error) {
//	defer func() {
//		err := recover()
//		if er, ok := err.(*compileErr); ok {
//			code = nil
//			e = er
//		} else {
//			panic(err)
//		}
//	}()
//	return w.MustCompileExpr(src), nil
//}

// Compile, with panic on error
//func (w *World) MustCompile(src string) Expr {
//	Expr := "func(){" + src + "\n}" // wrap in func to turn into expression
//	tree, e := parser.ParseExpr(Expr)
//	if e != nil {
//		panic(err(fmt.Sprint(e)))
//	}
//
//	stmts := tree.(*ast.FuncLit).Body.List // strip func again
//	if w.Debug {
//		ast.Print(nil, stmts)
//	}
//
//	block := new(blockStmt)
//	for _, s := range stmts {
//		block.append(w.compileStmt(s))
//	}
//	return block
//}

// compiles source consisting of a number of statements. E.g.:
// 	src = "a = 1; b = sin(x)"
// 	code, err := world.Compile(src)
// 	code.Exec()
func (w *World) Compile(src string) (code Expr, e error) {

	// parse
	exprSrc := "func(){" + src + "\n}" // wrap in func to turn into expression
	tree, err := parser.ParseExpr(exprSrc)
	if err != nil {
		return nil, fmt.Errorf("script syntax error: line %v", err)
	}

	// compile
	defer func() {
		err := recover()
		if compErr, ok := err.(*compileErr); ok {
			code = nil
			e = fmt.Errorf("%v: %v", pos2line(compErr.pos, exprSrc), compErr.msg)
		} else {
			panic(err)
		}
	}()

	stmts := tree.(*ast.FuncLit).Body.List // strip func again
	if w.Debug {
		ast.Print(nil, stmts)
	}

	block := new(blockStmt)
	for _, s := range stmts {
		block.append(w.compileStmt(s))
	}
	return block, nil
}

func pos2line(pos token.Pos, src string) string {
	lines := strings.Split(src, "\n")
	line := 0
	for i, b := range src {
		if token.Pos(i) == pos {
			return fmt.Sprint("line", line, lines[line])
		}
		if b == '\n' {
			line++
		}
	}
	return fmt.Sprint("position", pos) // we should not reach this
}
