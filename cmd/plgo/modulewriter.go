package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ToUnexported changes Exported function name to unexported
func ToUnexported(name string) string {
	return strings.ToLower(name[0:1]) + name[1:]
}

// ModuleWriter writes the tmp module wrapper that will be build to shared object
type ModuleWriter struct {
	PackageName string
	Doc         string
	fset        *token.FileSet
	packageAst  *ast.Package
	functions   []CodeWriter
}

// NewModuleWriter parses the go package and returns the FileSet and AST
func NewModuleWriter(packagePath string) (*ModuleWriter, error) {
	fset := token.NewFileSet()
	// skip _test files in current package
	filtertestfiles := func(fi os.FileInfo) bool {
		if strings.HasSuffix(fi.Name(), "_test.go") {
			return false
		}
		return true
	}

	f, err := parser.ParseDir(fset, packagePath, filtertestfiles, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("Cannot parse package: %w", err)
	}
	if len(f) > 1 {
		return nil, fmt.Errorf("More than one package in %s", packagePath)
	}
	packageAst, ok := f["main"]
	if !ok {
		return nil, fmt.Errorf("No package main in %s", packagePath)
	}
	var packageDoc string
	for _, packageFile := range packageAst.Files {
		packageDoc += packageFile.Doc.Text() + "\n"
	}
	// collect functions from the package
	funcVisitor := new(FuncVisitor)
	ast.Walk(funcVisitor, packageAst)
	if funcVisitor.err != nil {
		return nil, funcVisitor.err
	}
	absPackagePath, err := filepath.Abs(packagePath)
	if err != nil {
		return nil, err
	}
	packageName := filepath.Base(absPackagePath)
	return &ModuleWriter{PackageName: packageName, Doc: packageDoc, fset: fset, packageAst: packageAst, functions: funcVisitor.functions}, nil
}

// WriteModule writes the tmp module wrapper
func (mw *ModuleWriter) WriteModule() (string, error) {
	tempPackagePath, err := buildPath()
	if err != nil {
		return "", fmt.Errorf("Cannot get tempdir: %w", err)
	}
	// Create a go.mod for the temp build directory.
	// Start from the user's go.mod (to preserve dependencies), but change
	// the module name and remove the plgo dependency (pl.go is inlined).
	if err := mw.writeBuildGoMod(tempPackagePath); err != nil {
		return "", err
	}
	err = mw.writeUserPackage(tempPackagePath)
	if err != nil {
		return "", err
	}
	err = mw.writeplgo(tempPackagePath)
	if err != nil {
		return "", err
	}
	return tempPackagePath, nil
}

func (mw *ModuleWriter) writeBuildGoMod(tempPackagePath string) error {
	absPackagePath, _ := filepath.Abs(".")
	srcGoMod := filepath.Join(absPackagePath, "go.mod")
	srcGoSum := filepath.Join(absPackagePath, "go.sum")

	// Read the user's go.mod
	data, err := os.ReadFile(srcGoMod)
	if err != nil {
		// No go.mod — write a minimal one
		gomod := []byte("module plgo_build\n\ngo 1.22\n")
		return os.WriteFile(filepath.Join(tempPackagePath, "go.mod"), gomod, 0o644)
	}

	// Replace module name and remove plgo require/replace lines
	lines := strings.Split(string(data), "\n")
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Replace module declaration
		if strings.HasPrefix(trimmed, "module ") {
			out = append(out, "module plgo_build")
			continue
		}
		// Skip plgo dependency lines
		if strings.Contains(trimmed, "github.com/mgr43/plgo") {
			continue
		}
		out = append(out, line)
	}
	if err := os.WriteFile(filepath.Join(tempPackagePath, "go.mod"), []byte(strings.Join(out, "\n")), 0o644); err != nil {
		return err
	}

	// Copy go.sum if it exists
	if sumData, err := os.ReadFile(srcGoSum); err == nil {
		_ = os.WriteFile(filepath.Join(tempPackagePath, "go.sum"), sumData, 0o644)
	}
	return nil
}

func (mw *ModuleWriter) writeUserPackage(tempPackagePath string) error {
	ast.Walk(new(Remover), mw.packageAst)
	packageFile, err := os.Create(filepath.Join(tempPackagePath, "package.go"))
	if err != nil {
		return fmt.Errorf("Cannot write file tempdir: %w", err)
	}
	if err = format.Node(packageFile, mw.fset, ast.MergePackageFiles(mw.packageAst, ast.FilterFuncDuplicates)); err != nil {
		return fmt.Errorf("Cannot format package %w", err)
	}
	err = packageFile.Close()
	if err != nil {
		return fmt.Errorf("Cannot write file tempdir: %w", err)
	}
	return nil
}

//go:embed pl.go.src
var plgoSource string

func (mw *ModuleWriter) writeplgo(tempPackagePath string) error {
	// Replace "package plgo" with "package main", handling any leading godoc comments
	plgoSource = strings.Replace(plgoSource, "package plgo", "package main", 1)
	postgresIncludeDir, err := exec.Command("pg_config", "--includedir-server").CombinedOutput()
	if err != nil {
		return fmt.Errorf("Cannot run pg_config: %w", err)
	}
	postgresIncludeStr := getcorrectpath(string(postgresIncludeDir)) // corrects 8.3 filenames on windows
	plgoSource = strings.Replace(plgoSource, "/usr/include/postgresql/server", postgresIncludeStr, 1)

	addOtherIncludesAndLDFLAGS(&plgoSource, postgresIncludeStr) // on mingw windows workarounds

	// Remove the funcdec and pgmodulemagic placeholders — they go in funcdec.go
	plgoSource = strings.Replace(plgoSource, "//{funcdec}", "", 1)
	plgoSource = strings.Replace(plgoSource, "//{pgmodulemagic}", "", 1)

	// Generate //export wrapper functions and inject them into pl.go.
	// They must be in the same file as the CGo type definitions so that
	// Datum and funcInfo are visible to CGo's //export type checker.
	var exportedFuncs bytes.Buffer
	for _, f := range mw.functions {
		f.Code(&exportedFuncs)
	}
	plgoSource = strings.Replace(plgoSource, "//{exportedfuncs}", exportedFuncs.String(), 1)

	err = os.WriteFile(filepath.Join(tempPackagePath, "pl.go"), []byte(plgoSource), 0o644)
	if err != nil {
		return fmt.Errorf("Cannot write file tempdir: %w", err)
	}

	// Write funcdec.go — a separate file with PG_MODULE_MAGIC and
	// PG_FUNCTION_INFO_V1 macros. This must be separate from pl.go because
	// CGo generates duplicate translation units for files with //export
	// directives, and these macros create non-static C functions that would clash.
	var funcdec string
	for _, f := range mw.functions {
		funcdec += f.FuncDec() + "\n"
	}
	funcdecSource := `package main

/*
#include "postgres.h"
#include "fmgr.h"
#cgo CFLAGS: -I"` + postgresIncludeStr + `"

#ifdef PG_MODULE_MAGIC
PG_MODULE_MAGIC;
#endif

` + funcdec + `
*/
import "C"
`
	err = os.WriteFile(filepath.Join(tempPackagePath, "funcdec.go"), []byte(funcdecSource), 0o644)
	if err != nil {
		return fmt.Errorf("Cannot write funcdec.go: %w", err)
	}

	return nil
}

// WriteSQL writes sql file with commands to create functions in DB
func (mw *ModuleWriter) WriteSQL(tempPackagePath string) error {
	sqlPath := filepath.Join(tempPackagePath, mw.PackageName+"--0.1.sql")
	sqlFile, err := os.Create(sqlPath)
	if err != nil {
		return err
	}
	defer sqlFile.Close()
	sqlFile.WriteString(`-- complain if script is sourced in psql, rather than via CREATE EXTENSION
\echo Use "CREATE EXTENSION ` + mw.PackageName + `" to load this file. \quit
`)
	for _, f := range mw.functions {
		f.SQL(mw.PackageName, sqlFile)
	}
	return nil
}

// WriteControl writes .control file for the new postgresql extension
func (mw *ModuleWriter) WriteControl(path string) error {
	control := []byte(`# ` + mw.PackageName + ` extension
comment = '` + mw.PackageName + ` extension'
default_version = '0.1'
relocatable = true`)
	controlPath := filepath.Join(path, mw.PackageName+".control")
	return os.WriteFile(controlPath, control, 0o644)
}

// WriteMakefile writes Makefile for the new postgresql extension
func (mw *ModuleWriter) WriteMakefile(path string) error {
	makefile := []byte(`EXTENSION = ` + mw.PackageName + `
DATA = ` + mw.PackageName + `--0.1.sql

PG_CONFIG ?= pg_config

SHAREDIR = $(shell $(PG_CONFIG) --sharedir)
PKGLIBDIR = $(shell $(PG_CONFIG) --pkglibdir)

install:
	install -d $(DESTDIR)$(SHAREDIR)/extension
	install -m 644 $(EXTENSION).control $(DESTDIR)$(SHAREDIR)/extension/
	install -m 644 $(DATA) $(DESTDIR)$(SHAREDIR)/extension/
	install -d $(DESTDIR)$(PKGLIBDIR)
	install -m 755 $(EXTENSION).so $(DESTDIR)$(PKGLIBDIR)/
`)
	makePath := filepath.Join(path, "Makefile")
	return os.WriteFile(makePath, makefile, 0o644)
}
