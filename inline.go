package main

import (
	"errors"
	"fmt"
	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/dave/dst/dstutil"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

var (
	errMultiPkgs = errors.New("having multiple packages in the same directory is not supported")
)

func inlinePackage(dir string, outDir string, suffix string) error {
	if len(outDir) == 0 {
		outDir = dir
	}
	pkgMap, err := decorator.ParseDir(token.NewFileSet(), dir, func(i os.FileInfo) bool {
		return !strings.HasSuffix(trimFileExtension(i.Name()), "_"+suffix)
	}, parser.ParseComments)
	if err != nil {
		return err
	}

	if len(pkgMap) > 1 {
		return errMultiPkgs
	}

	for _, pkg := range pkgMap {
		funcMap := extractInlineFuncs(pkg)

		for fn, f := range pkg.Files {
			inlineFunc(funcMap, f)

			ofn := fmt.Sprintf("%s_%s.go", filepath.Join(outDir, trimFileExtension(filepath.Base(fn))), suffix)
			of, err := os.Create(ofn)
			if err != nil {
				return err
			}
			err = decorator.Fprint(of, f)
			_ = of.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// extractInlineFuncs extracts dst.FuncDecl from all functions marked with [always_inline]
// and deletes those functions from the source code.
func extractInlineFuncs(pkg *dst.Package) map[string]*dst.FuncDecl {
	rMap := make(map[string]*dst.FuncDecl)
	for _, f := range pkg.Files {
		dstutil.Apply(f, func(cursor *dstutil.Cursor) bool {
			n := cursor.Node()
			switch n := n.(type) {
			case *dst.FuncDecl:
				// Yep a function declaration
				var inline bool
				for _, dec := range n.Decorations().Start.All() {
					if strings.Contains(dec, "[always_inline]") {
						inline = true
					}
				}
				if !inline {
					return true
				}
				// Store the function in a map
				rMap[n.Name.Name] = dst.Clone(n).(*dst.FuncDecl)
				// Remove it from the source
				cursor.Delete()
				return false
			}
			return true
		}, nil)
	}
	return rMap
}

// flattenFields flattens fields, so that each field contains up to only one name.
// Go has two ways of representing function parameters or return values:
// (x int, y int, z int)
// or
// (x, y, z int)
// These two forms are stored differently in the AST. We convert them all to the former one.
func flattenFields(fields []*dst.Field) []*dst.Field {
	newFields := make([]*dst.Field, 0, len(fields))
	for _, f := range fields {
		if len(f.Names) == 0 {
			// A single anonymous field
			newFields = append(newFields, f)
		} else {
			// One or more fields with names
			for _, n := range f.Names {
				newFields = append(newFields, &dst.Field{
					Names: []*dst.Ident{n},
					Type:  f.Type,
					Tag:   f.Tag,
					Decs:  f.Decs,
				})
			}
		}
	}
	return newFields
}

func inlineFunc(inlineFuncMap map[string]*dst.FuncDecl, n dst.Node) dst.Node {
	var funcIdx int
	return dstutil.Apply(n, func(cursor *dstutil.Cursor) bool {
		n := cursor.Node()
		switch n := n.(type) {
		case *dst.AssignStmt:
			// a = foo()
			callExpr, ok := n.Rhs[0].(*dst.CallExpr)
			if !ok {
				return true
			}
			funcDecl := callToFuncDecl(inlineFuncMap, callExpr)
			if funcDecl == nil {
				return true
			}
			// Recurse to handle nested inlines
			funcDecl = inlineFunc(inlineFuncMap, funcDecl).(*dst.FuncDecl)

			if len(n.Rhs) > 1 {
				panic("having more than a single RHS to a CallExpr is not supported for now")
			}

			if n.Tok == token.DEFINE {
				newDefinitions := &dst.GenDecl{
					Tok:   token.VAR,
					Specs: make([]dst.Spec, len(n.Lhs)),
				}

				frList := flattenFields(funcDecl.Type.Results.List)
				for i, e := range n.Lhs {
					newDefinitions.Specs[i] = &dst.ValueSpec{
						Names: []*dst.Ident{dst.NewIdent(e.(*dst.Ident).Name)},
						Type:  dst.Clone(frList[i].Type).(dst.Expr),
					}
				}

				cursor.InsertBefore(&dst.DeclStmt{Decl: newDefinitions})
			}

			retValDeclStmt, retValNames := extractReturnValues(funcDecl)
			inlinedStatements := &dst.BlockStmt{
				List: []dst.Stmt{retValDeclStmt},
			}
			body := dst.Clone(funcDecl.Body).(*dst.BlockStmt)

			body = replaceReturnStatements(funcDecl.Name.Name, funcIdx, body, func(stmt *dst.ReturnStmt) dst.Stmt {
				returnAssignmentSpecs := make([]dst.Stmt, len(retValNames))
				for i := range retValNames {
					returnAssignmentSpecs[i] = &dst.AssignStmt{
						Lhs: []dst.Expr{dst.NewIdent(retValNames[i])},
						Tok: token.ASSIGN,
						Rhs: []dst.Expr{stmt.Results[i]},
					}
				}
				// Replace the return with the new assignments
				return &dst.BlockStmt{List: returnAssignmentSpecs}
			})
			// Reassign input parameters to formal parameters
			reassignmentStmt := getFormalParamReassignments(funcDecl, callExpr)
			inlinedStatements.List = append(inlinedStatements.List, &dst.BlockStmt{
				List: append([]dst.Stmt{reassignmentStmt}, body.List...),
			})
			// Assign mangled return values to the original assignment variables
			newAssignment := dst.Clone(n).(*dst.AssignStmt)
			newAssignment.Tok = token.ASSIGN
			newAssignment.Rhs = make([]dst.Expr, len(retValNames))
			for i := range retValNames {
				newAssignment.Rhs[i] = dst.NewIdent(retValNames[i])
			}
			inlinedStatements.List = append(inlinedStatements.List, newAssignment)

			cursor.Replace(inlinedStatements)

		case *dst.ExprStmt:
			// foo()
			callExpr, ok := n.X.(*dst.CallExpr)
			if !ok {
				return true
			}
			funcDecl := callToFuncDecl(inlineFuncMap, callExpr)
			if funcDecl == nil {
				return true
			}
			// Recurse to handle nested inlines
			funcDecl = inlineFunc(inlineFuncMap, funcDecl).(*dst.FuncDecl)

			reassignments := getFormalParamReassignments(funcDecl, callExpr)

			// No need to handle return values
			funcBlock := &dst.BlockStmt{
				List: []dst.Stmt{reassignments},
			}
			body := dst.Clone(funcDecl.Body).(*dst.BlockStmt)

			// Remove return values if there are any
			body = replaceReturnStatements(funcDecl.Name.Name, funcIdx, body, nil)
			// Add the inlined function body to the block
			funcBlock.List = append(funcBlock.List, body.List...)

			cursor.Replace(funcBlock)

		default:
			return true
		}
		funcIdx++
		return true
	}, nil)
}

func callToFuncDecl(funcMap map[string]*dst.FuncDecl, n *dst.CallExpr) *dst.FuncDecl {
	ident, ok := n.Fun.(*dst.Ident)
	if !ok {
		return nil
	}

	info, ok := funcMap[ident.Name]
	if !ok {
		return nil
	}
	return info
}

// extractReturnValues generates return value variables. It will produce one
// statement per return value of the input FuncDecl. For example, for
// a FuncDecl that returns two boolean arguments, lastVal and lastValNull,
// two statements will be returned:
//	var _rv_lastVal bool
//	var _rv_lastValNull bool
// The second return is a slice of the names of each of the mangled return
// declarations, in this example, _rv_lastVal and _rv_lastValNull.
func extractReturnValues(decl *dst.FuncDecl) (retValDeclStmt dst.Stmt, retValNames []string) {
	if decl.Type.Results == nil {
		return &dst.EmptyStmt{}, nil
	}
	results := flattenFields(decl.Type.Results.List)
	retValNames = make([]string, len(results))
	specs := make([]dst.Spec, len(results))
	for i, result := range results {
		var retvalName string
		if len(result.Names) == 0 {
			retvalName = fmt.Sprintf("_rv_%d", i)
		} else {
			retvalName = fmt.Sprintf("_rv_%s", result.Names[0])
		}
		retValNames[i] = retvalName
		specs[i] = &dst.ValueSpec{
			Names: []*dst.Ident{dst.NewIdent(retvalName)},
			Type:  dst.Clone(result.Type).(dst.Expr),
		}
	}
	return &dst.DeclStmt{
		Decl: &dst.GenDecl{
			Tok:   token.VAR,
			Specs: specs,
		},
	}, retValNames
}

// getFormalParamReassignments creates a new DEFINE (:=) statement per parameter
// to a FuncDecl, which makes a fresh variable with the same name as the formal
// parameter name and assigns it to the corresponding name in the CallExpr.
//
// For example, given a FuncDecl:
//
// func foo(a int, b string) { ... }
//
// and a CallExpr
//
// foo(x, y)
//
// we'll return the statement:
//
// var (
//   a int = x
//   b string = y
// )
//
// In the case where the formal parameter name is the same as the input
// parameter name, no extra assignment is created.
func getFormalParamReassignments(decl *dst.FuncDecl, callExpr *dst.CallExpr) dst.Stmt {
	formalParams := flattenFields(decl.Type.Params.List)
	reassignmentSpecs := make([]dst.Spec, 0, len(formalParams))
	for i, formalParam := range formalParams {
		reassignmentSpecs = append(reassignmentSpecs, &dst.ValueSpec{
			Names:  []*dst.Ident{dst.NewIdent(formalParam.Names[0].Name)},
			Type:   dst.Clone(formalParam.Type).(dst.Expr),
			Values: []dst.Expr{callExpr.Args[i]},
		})
	}
	if len(reassignmentSpecs) == 0 {
		return &dst.EmptyStmt{}
	}
	return &dst.DeclStmt{
		Decl: &dst.GenDecl{
			Tok:   token.VAR,
			Specs: reassignmentSpecs,
		},
	}
}

// replaceReturnStatements edits the input BlockStmt, from the function funcName,
// replacing ReturnStmts at the end of the BlockStmts with the results of
// applying returnEditor on the ReturnStmt or deleting them if the modifier is
// nil.
// It will panic if any return statements are not in the final position of the
// input block.
func replaceReturnStatements(
	funcName string, funcIdx int, stmt *dst.BlockStmt, returnModifier func(*dst.ReturnStmt) dst.Stmt,
) *dst.BlockStmt {
	if len(stmt.List) == 0 {
		return stmt
	}
	// Insert an explicit return at the end if there isn't one
	// We'll need to edit this later to make early returns work properly
	lastStmt := stmt.List[len(stmt.List)-1]
	if _, ok := lastStmt.(*dst.ReturnStmt); !ok {
		ret := &dst.ReturnStmt{}
		stmt.List = append(stmt.List, ret)
		lastStmt = ret
	}
	retStmt := lastStmt.(*dst.ReturnStmt)
	if returnModifier == nil {
		stmt.List[len(stmt.List)-1] = &dst.EmptyStmt{}
	} else {
		stmt.List[len(stmt.List)-1] = returnModifier(retStmt)
	}

	label := dst.NewIdent(fmt.Sprintf("%s_return_%d", funcName, funcIdx))

	// Replace early returns with gotos
	var foundInlineReturn bool
	stmt = dstutil.Apply(stmt, func(cursor *dstutil.Cursor) bool {
		n := cursor.Node()
		switch n := n.(type) {
		case *dst.FuncLit:
			// A FuncLit is a function literal, like:
			// x := func() int { return 3 }
			// We don't recurse into function literals since the return statements
			// they contain aren't relevant to the inliner
			return false
		case *dst.ReturnStmt:
			foundInlineReturn = true
			gotoStmt := &dst.BranchStmt{
				Tok:   token.GOTO,
				Label: dst.Clone(label).(*dst.Ident),
			}
			if returnModifier != nil {
				cursor.Replace(returnModifier(n))
				cursor.InsertAfter(gotoStmt)
			} else {
				cursor.Replace(gotoStmt)
			}
			return false
		}
		return true
	}, nil).(*dst.BlockStmt)

	if foundInlineReturn {
		// Add the label at the end
		stmt.List = append(stmt.List,
			&dst.LabeledStmt{
				Label: label,
				Stmt:  &dst.EmptyStmt{Implicit: true},
			})
	}
	return stmt
}
