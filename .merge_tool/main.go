// gomerge merges multiple Go files into one output file.
// Usage: gomerge <out> <in1> [in2 ...]
// It unions import paths and concatenates all declarations (types, funcs, vars).
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"sort"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: gomerge <out> <in1> [in2...]")
		os.Exit(1)
	}
	out := os.Args[1]
	fset := token.NewFileSet()

	var pkgName string
	imports := map[string]string{} // import path -> alias
	var decls []ast.Decl

	for _, path := range os.Args[2:] {
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			fmt.Fprintln(os.Stderr, "parse", path, ":", err)
			os.Exit(1)
		}
		if pkgName == "" {
			pkgName = f.Name.Name
		}
		for _, imp := range f.Imports {
			p := imp.Path.Value
			alias := ""
			if imp.Name != nil {
				alias = imp.Name.Name
			}
			imports[p] = alias
		}
		for _, d := range f.Decls {
			if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
				continue
			}
			decls = append(decls, d)
		}
	}

	var buf bytes.Buffer
	buf.WriteString("package " + pkgName + "\n\n")
	if len(imports) > 0 {
		buf.WriteString("import (\n")
		paths := make([]string, 0, len(imports))
		for p := range imports {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			if imports[p] != "" {
				buf.WriteString("\t" + imports[p] + " " + p + "\n")
			} else {
				buf.WriteString("\t" + p + "\n")
			}
		}
		buf.WriteString(")\n\n")
	}

	cfg := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	for _, d := range decls {
		if err := cfg.Fprint(&buf, fset, d); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		buf.WriteString("\n\n")
	}

	src, err := format.Source(buf.Bytes())
	if err != nil {
		fmt.Fprintln(os.Stderr, "format:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(out, src, 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("merged", len(decls), "decls into", out)
}
