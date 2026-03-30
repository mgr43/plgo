package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// --- datumTypes map tests ---

func TestDatumTypesCompleteness(t *testing.T) {
	// Every scalar type should have a corresponding array type (except error, TriggerRow)
	scalars := []string{
		"string", "int16", "uint16", "int32", "uint32",
		"int64", "int", "uint", "float32", "float64", "bool", "time.Time",
	}
	for _, s := range scalars {
		if _, ok := datumTypes[s]; !ok {
			t.Errorf("missing scalar type %q in datumTypes", s)
		}
		if _, ok := datumTypes["[]"+s]; !ok {
			t.Errorf("missing array type []%s in datumTypes", s)
		}
	}
	// Special entries
	if _, ok := datumTypes["[]byte"]; !ok {
		t.Error("missing []byte in datumTypes")
	}
	if _, ok := datumTypes["TriggerRow"]; !ok {
		t.Error("missing TriggerRow in datumTypes")
	}
}

// --- NewCode / function classification tests ---

func parseFuncDecl(t *testing.T, src string) *ast.FuncDecl {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	for _, decl := range file.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok {
			return fd
		}
	}
	t.Fatal("no func decl found")
	return nil
}

func TestNewCode_VoidFunction(t *testing.T) {
	fd := parseFuncDecl(t, `package main
// Meh does nothing
func Meh() {}`)
	code, err := NewCode(fd)
	if err != nil {
		t.Fatal(err)
	}
	vf, ok := code.(*VoidFunction)
	if !ok {
		t.Fatalf("expected *VoidFunction, got %T", code)
	}
	if vf.Name != "Meh" {
		t.Errorf("name = %q, want Meh", vf.Name)
	}
	if len(vf.Params) != 0 {
		t.Errorf("params = %v, want empty", vf.Params)
	}
}

func TestNewCode_FunctionWithParams(t *testing.T) {
	fd := parseFuncDecl(t, `package main
func Add(a, b int64) int64 { return a + b }`)
	code, err := NewCode(fd)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := code.(*Function)
	if !ok {
		t.Fatalf("expected *Function, got %T", code)
	}
	if f.Name != "Add" {
		t.Errorf("name = %q, want Add", f.Name)
	}
	if len(f.Params) != 2 {
		t.Fatalf("params count = %d, want 2", len(f.Params))
	}
	if f.Params[0].Name != "a" || f.Params[0].Type != "int64" {
		t.Errorf("param[0] = %+v", f.Params[0])
	}
	if f.Params[1].Name != "b" || f.Params[1].Type != "int64" {
		t.Errorf("param[1] = %+v", f.Params[1])
	}
	if f.ReturnType != "int64" {
		t.Errorf("return type = %q, want int64", f.ReturnType)
	}
	if f.IsStar {
		t.Error("IsStar should be false")
	}
}

func TestNewCode_FunctionReturnsArray(t *testing.T) {
	fd := parseFuncDecl(t, `package main
func GetNames() []string { return nil }`)
	code, err := NewCode(fd)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := code.(*Function)
	if !ok {
		t.Fatalf("expected *Function, got %T", code)
	}
	if f.ReturnType != "[]string" {
		t.Errorf("return type = %q, want []string", f.ReturnType)
	}
}

func TestNewCode_FunctionReturnsStar(t *testing.T) {
	fd := parseFuncDecl(t, `package main
func MaybeInt() *int64 { return nil }`)
	code, err := NewCode(fd)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := code.(*Function)
	if !ok {
		t.Fatalf("expected *Function, got %T", code)
	}
	if f.ReturnType != "int64" {
		t.Errorf("return type = %q, want int64", f.ReturnType)
	}
	if !f.IsStar {
		t.Error("IsStar should be true")
	}
}

func TestNewCode_TriggerFunction(t *testing.T) {
	fd := parseFuncDecl(t, `package main
import "gitlab.com/microo8/plgo"
func MyTrigger(td *plgo.TriggerData) *plgo.TriggerRow { return td.NewRow }`)
	code, err := NewCode(fd)
	if err != nil {
		t.Fatal(err)
	}
	tf, ok := code.(*TriggerFunction)
	if !ok {
		t.Fatalf("expected *TriggerFunction, got %T", code)
	}
	if tf.Name != "MyTrigger" {
		t.Errorf("name = %q, want MyTrigger", tf.Name)
	}
	if len(tf.Params) != 0 {
		t.Errorf("trigger params (after removing TriggerData) = %v, want empty", tf.Params)
	}
}

// --- Error cases ---

func TestNewCode_MultipleReturnTypes(t *testing.T) {
	fd := parseFuncDecl(t, `package main
func Bad() (string, error) { return "", nil }`)
	_, err := NewCode(fd)
	if err == nil {
		t.Fatal("expected error for multiple return types")
	}
	if !strings.Contains(err.Error(), "multiple return types") {
		t.Errorf("error = %q, want 'multiple return types'", err.Error())
	}
}

func TestNewCode_UnsupportedParamType(t *testing.T) {
	fd := parseFuncDecl(t, `package main
type Custom struct{}
func Bad(c Custom) {}`)
	_, err := NewCode(fd)
	if err == nil {
		t.Fatal("expected error for unsupported param type")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("error = %q, want 'not supported'", err.Error())
	}
}

func TestNewCode_UnsupportedReturnType(t *testing.T) {
	fd := parseFuncDecl(t, `package main
type Custom struct{}
func Bad() Custom { return Custom{} }`)
	_, err := NewCode(fd)
	if err == nil {
		t.Fatal("expected error for unsupported return type")
	}
}

// --- FuncDec tests ---

func TestVoidFunction_FuncDec(t *testing.T) {
	f := &VoidFunction{Name: "Meh"}
	dec := f.FuncDec()
	expected := "PG_FUNCTION_INFO_V1(Meh);"
	if dec != expected {
		t.Errorf("FuncDec() = %q, want %q", dec, expected)
	}
}

// --- SQL generation tests ---

func TestVoidFunction_SQL(t *testing.T) {
	f := &VoidFunction{Name: "Meh", Params: nil}
	var buf bytes.Buffer
	f.SQL("myext", &buf)
	sql := buf.String()
	if !strings.Contains(sql, "CREATE OR REPLACE FUNCTION Meh()") {
		t.Errorf("missing CREATE FUNCTION, got:\n%s", sql)
	}
	if !strings.Contains(sql, "RETURNS VOID") {
		t.Errorf("missing RETURNS VOID, got:\n%s", sql)
	}
	if !strings.Contains(sql, "'$libdir/myext', 'Meh'") {
		t.Errorf("missing $libdir reference, got:\n%s", sql)
	}
	if !strings.Contains(sql, "LANGUAGE c VOLATILE STRICT") {
		t.Errorf("missing LANGUAGE c, got:\n%s", sql)
	}
}

func TestFunction_SQL_WithParams(t *testing.T) {
	f := &Function{
		VoidFunction: VoidFunction{
			Name:   "ConcatAll",
			Params: []Param{{Name: "tableName", Type: "string"}, {Name: "colName", Type: "string"}},
		},
		ReturnType: "string",
	}
	var buf bytes.Buffer
	f.SQL("myext", &buf)
	sql := buf.String()
	if !strings.Contains(sql, "tableName text,colName text") {
		t.Errorf("missing param types, got:\n%s", sql)
	}
	if !strings.Contains(sql, "RETURNS text") {
		t.Errorf("missing RETURNS text, got:\n%s", sql)
	}
}

func TestFunction_SQL_ArrayReturn(t *testing.T) {
	f := &Function{
		VoidFunction: VoidFunction{Name: "GetInts"},
		ReturnType:   "[]int64",
	}
	var buf bytes.Buffer
	f.SQL("myext", &buf)
	sql := buf.String()
	if !strings.Contains(sql, "RETURNS bigint[]") {
		t.Errorf("missing RETURNS bigint[], got:\n%s", sql)
	}
}

func TestFunction_SQL_ByteaReturn(t *testing.T) {
	f := &Function{
		VoidFunction: VoidFunction{Name: "Compress", Params: []Param{{Name: "data", Type: "[]byte"}}},
		ReturnType:   "[]byte",
	}
	var buf bytes.Buffer
	f.SQL("myext", &buf)
	sql := buf.String()
	if !strings.Contains(sql, "RETURNS bytea") {
		t.Errorf("missing RETURNS bytea, got:\n%s", sql)
	}
	if !strings.Contains(sql, "data bytea") {
		t.Errorf("missing data bytea param, got:\n%s", sql)
	}
}

func TestTriggerFunction_SQL(t *testing.T) {
	f := &TriggerFunction{VoidFunction: VoidFunction{Name: "MyTrigger"}}
	var buf bytes.Buffer
	f.SQL("myext", &buf)
	sql := buf.String()
	if !strings.Contains(sql, "RETURNS TRIGGER") {
		t.Errorf("missing RETURNS TRIGGER, got:\n%s", sql)
	}
}

func TestFunction_SQL_Comment(t *testing.T) {
	f := &VoidFunction{Name: "Meh", Doc: "Does nothing useful"}
	var buf bytes.Buffer
	f.SQL("myext", &buf)
	sql := buf.String()
	if !strings.Contains(sql, "COMMENT ON FUNCTION") {
		t.Errorf("missing COMMENT, got:\n%s", sql)
	}
	if !strings.Contains(sql, "Does nothing useful") {
		t.Errorf("missing doc string, got:\n%s", sql)
	}
}

// --- Code generation tests ---

func TestVoidFunction_Code(t *testing.T) {
	f := &VoidFunction{
		Name:   "Meh",
		Params: []Param{{Name: "name", Type: "string"}},
	}
	var buf bytes.Buffer
	f.Code(&buf)
	code := buf.String()
	if !strings.Contains(code, "//export Meh") {
		t.Errorf("missing //export, got:\n%s", code)
	}
	if !strings.Contains(code, "var name string") {
		t.Errorf("missing param declaration, got:\n%s", code)
	}
	if !strings.Contains(code, "fcinfo.Scan(") {
		t.Errorf("missing fcinfo.Scan, got:\n%s", code)
	}
	if !strings.Contains(code, "__Meh(") {
		t.Errorf("missing __Meh call, got:\n%s", code)
	}
	if !strings.Contains(code, "return toDatum(nil)") {
		t.Errorf("missing return toDatum(nil), got:\n%s", code)
	}
}

func TestFunction_Code_WithReturn(t *testing.T) {
	f := &Function{
		VoidFunction: VoidFunction{
			Name:   "Double",
			Params: []Param{{Name: "x", Type: "int64"}},
		},
		ReturnType: "int64",
	}
	var buf bytes.Buffer
	f.Code(&buf)
	code := buf.String()
	if !strings.Contains(code, "ret := __Double(") {
		t.Errorf("missing ret := __Double, got:\n%s", code)
	}
	if !strings.Contains(code, "return toDatum(ret)") {
		t.Errorf("missing return toDatum(ret), got:\n%s", code)
	}
}

func TestFunction_Code_StarReturn(t *testing.T) {
	f := &Function{
		VoidFunction: VoidFunction{Name: "MaybeInt"},
		ReturnType:   "int64",
		IsStar:       true,
	}
	var buf bytes.Buffer
	f.Code(&buf)
	code := buf.String()
	if !strings.Contains(code, "ret==nil") {
		t.Errorf("missing nil check for star return, got:\n%s", code)
	}
	if !strings.Contains(code, "return toDatum(*ret)") {
		t.Errorf("missing return toDatum(*ret), got:\n%s", code)
	}
}

// --- Remover visitor tests ---

func TestRemover_StripsPlgoImport(t *testing.T) {
	fset := token.NewFileSet()
	src := `package main

import "gitlab.com/microo8/plgo"

func Foo() { plgo.Open() }
`
	file, err := parser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Walk(new(Remover), file)

	for _, imp := range file.Imports {
		if imp.Path.Value == "\"gitlab.com/microo8/plgo\"" {
			t.Error("Remover did not clear plgo import path")
		}
	}
}

// --- NewModuleWriter tests ---

func TestNewModuleWriter_Example(t *testing.T) {
	mw, err := NewModuleWriter("../../example")
	if err != nil {
		t.Fatal(err)
	}
	if mw.PackageName != "example" {
		t.Errorf("PackageName = %q, want example", mw.PackageName)
	}
	// Should find: Meh, ConcatAll, CreatedTimeTrigger, ConcatArray, GzipCompress, DoubleInt, ScaleArray, MaybeUpper, GenerateSeries, RepeatString
	if len(mw.functions) != 10 {
		names := make([]string, len(mw.functions))
		for i, f := range mw.functions {
			names[i] = f.FuncDec()
		}
		t.Errorf("expected 10 functions, got %d: %v", len(mw.functions), names)
	}

	// Verify function types
	// Note: names are the original exported names (before FuncVisitor renames to __)
	typeMap := map[string]string{}
	for _, f := range mw.functions {
		switch v := f.(type) {
		case *VoidFunction:
			typeMap[v.Name] = "void"
		case *Function:
			typeMap[v.Name] = "func:" + v.ReturnType
		case *TriggerFunction:
			typeMap[v.Name] = "trigger"
		case *SetOfFunction:
			typeMap[v.Name] = "setof:" + v.ElementType
		}
	}
	if typeMap["Meh"] != "void" {
		t.Errorf("Meh type = %q, want void", typeMap["Meh"])
	}
	if typeMap["ConcatAll"] != "func:string" {
		t.Errorf("ConcatAll type = %q, want func:string", typeMap["ConcatAll"])
	}
	if typeMap["CreatedTimeTrigger"] != "trigger" {
		t.Errorf("CreatedTimeTrigger type = %q, want trigger", typeMap["CreatedTimeTrigger"])
	}
	if typeMap["ConcatArray"] != "func:string" {
		t.Errorf("ConcatArray type = %q, want func:string", typeMap["ConcatArray"])
	}
	if typeMap["GzipCompress"] != "func:[]byte" {
		t.Errorf("GzipCompress type = %q, want func:[]byte", typeMap["GzipCompress"])
	}
	if typeMap["DoubleInt"] != "func:int32" {
		t.Errorf("DoubleInt type = %q, want func:int32", typeMap["DoubleInt"])
	}
	if typeMap["ScaleArray"] != "func:[]int64" {
		t.Errorf("ScaleArray type = %q, want func:[]int64", typeMap["ScaleArray"])
	}
	if typeMap["MaybeUpper"] != "func:string" {
		t.Errorf("MaybeUpper type = %q, want func:string", typeMap["MaybeUpper"])
	}
	if typeMap["GenerateSeries"] != "setof:int32" {
		t.Errorf("GenerateSeries type = %q, want setof:int32", typeMap["GenerateSeries"])
	}
	if typeMap["RepeatString"] != "setof:string" {
		t.Errorf("RepeatString type = %q, want setof:string", typeMap["RepeatString"])
	}
}

func TestNewModuleWriter_NonMainPackage(t *testing.T) {
	// The plgo/ directory itself is package main, but let's test with a temp dir
	// that has a non-main package — use the test's own directory trick
	_, err := NewModuleWriter("../../") // root is package plgo, not main
	if err == nil {
		t.Fatal("expected error for non-main package")
	}
	if !strings.Contains(err.Error(), "No package main") {
		t.Errorf("error = %q, want 'No package main'", err.Error())
	}
}

// --- ToUnexported tests ---

func TestToUnexported(t *testing.T) {
	tests := []struct{ in, want string }{
		{"ConcatAll", "concatAll"},
		{"A", "a"},
		{"PLGoTest", "pLGoTest"},
	}
	for _, tt := range tests {
		got := ToUnexported(tt.in)
		if got != tt.want {
			t.Errorf("ToUnexported(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// --- Per-type SQL and Code generation tests ---

func TestFunction_SQL_AllScalarTypes(t *testing.T) {
	tests := []struct {
		goType string
		pgType string
	}{
		{"string", "text"},
		{"int16", "smallint"},
		{"uint16", "smallint"},
		{"int32", "integer"},
		{"uint32", "integer"},
		{"int64", "bigint"},
		{"int", "bigint"},
		{"uint", "bigint"},
		{"float32", "real"},
		{"float64", "double precision"},
		{"bool", "boolean"},
	}
	for _, tt := range tests {
		t.Run(tt.goType+"_param", func(t *testing.T) {
			f := &Function{
				VoidFunction: VoidFunction{
					Name:   "Fn",
					Params: []Param{{Name: "v", Type: tt.goType}},
				},
				ReturnType: tt.goType,
			}
			var buf bytes.Buffer
			f.SQL("ext", &buf)
			sql := buf.String()
			if !strings.Contains(sql, "v "+tt.pgType) {
				t.Errorf("param: expected 'v %s', got:\n%s", tt.pgType, sql)
			}
			if !strings.Contains(sql, "RETURNS "+tt.pgType) {
				t.Errorf("return: expected 'RETURNS %s', got:\n%s", tt.pgType, sql)
			}
		})
	}
}

func TestFunction_SQL_AllArrayTypes(t *testing.T) {
	tests := []struct {
		goType   string
		pgType   string
		pgReturn string
	}{
		{"[]string", "text[]", "text[]"},
		{"[]int16", "smallint[]", "smallint[]"},
		{"[]uint16", "smallint[]", "smallint[]"},
		{"[]int32", "integer[]", "integer[]"},
		{"[]uint32", "integer[]", "integer[]"},
		{"[]int64", "bigint[]", "bigint[]"},
		{"[]int", "bigint[]", "bigint[]"},
		{"[]uint", "bigint[]", "bigint[]"},
		{"[]float32", "real[]", "real[]"},
		{"[]float64", "double precision[]", "double precision[]"},
		{"[]bool", "boolean[]", "boolean[]"},
	}
	for _, tt := range tests {
		t.Run(tt.goType+"_return", func(t *testing.T) {
			f := &Function{
				VoidFunction: VoidFunction{Name: "Fn"},
				ReturnType:   tt.goType,
			}
			var buf bytes.Buffer
			f.SQL("ext", &buf)
			sql := buf.String()
			if !strings.Contains(sql, "RETURNS "+tt.pgReturn) {
				t.Errorf("expected 'RETURNS %s', got:\n%s", tt.pgReturn, sql)
			}
		})
	}
}

func TestFunction_Code_AllScalarTypes(t *testing.T) {
	types := []string{
		"string", "int16", "uint16", "int32", "uint32",
		"int64", "int", "uint", "float32", "float64", "bool",
	}
	for _, goType := range types {
		t.Run(goType, func(t *testing.T) {
			f := &Function{
				VoidFunction: VoidFunction{
					Name:   "Fn",
					Params: []Param{{Name: "v", Type: goType}},
				},
				ReturnType: goType,
			}
			var buf bytes.Buffer
			f.Code(&buf)
			code := buf.String()
			if !strings.Contains(code, "var v "+goType) {
				t.Errorf("missing 'var v %s', got:\n%s", goType, code)
			}
			if !strings.Contains(code, "//export Fn") {
				t.Errorf("missing //export Fn")
			}
			if !strings.Contains(code, "ret := __Fn(") {
				t.Errorf("missing ret assignment")
			}
			if !strings.Contains(code, "return toDatum(ret)") {
				t.Errorf("missing return toDatum(ret)")
			}
			if !strings.Contains(code, "elog_error") {
				t.Errorf("missing elog_error error handler")
			}
		})
	}
}

func TestVoidFunction_Code_AllScalarParams(t *testing.T) {
	types := []string{
		"string", "int16", "uint16", "int32", "uint32",
		"int64", "int", "uint", "float32", "float64", "bool",
	}
	for _, goType := range types {
		t.Run(goType, func(t *testing.T) {
			f := &VoidFunction{
				Name:   "Fn",
				Params: []Param{{Name: "v", Type: goType}},
			}
			var buf bytes.Buffer
			f.Code(&buf)
			code := buf.String()
			if !strings.Contains(code, "var v "+goType) {
				t.Errorf("missing 'var v %s', got:\n%s", goType, code)
			}
			if !strings.Contains(code, "return toDatum(nil)") {
				t.Errorf("missing return toDatum(nil)")
			}
		})
	}
}

func TestNewCode_AllScalarParams(t *testing.T) {
	types := []string{
		"string", "int16", "uint16", "int32", "uint32",
		"int64", "int", "uint", "float32", "float64", "bool",
	}
	for _, goType := range types {
		t.Run(goType+"_param", func(t *testing.T) {
			fd := parseFuncDecl(t, `package main
func Fn(v `+goType+`) {}`)
			code, err := NewCode(fd)
			if err != nil {
				t.Fatal(err)
			}
			vf, ok := code.(*VoidFunction)
			if !ok {
				t.Fatalf("expected *VoidFunction, got %T", code)
			}
			if len(vf.Params) != 1 {
				t.Fatalf("params count = %d, want 1", len(vf.Params))
			}
			if vf.Params[0].Type != goType {
				t.Errorf("param type = %q, want %q", vf.Params[0].Type, goType)
			}
		})
		t.Run(goType+"_return", func(t *testing.T) {
			fd := parseFuncDecl(t, `package main
func Fn() `+goType+` { var z `+goType+`; return z }`)
			code, err := NewCode(fd)
			if err != nil {
				t.Fatal(err)
			}
			f, ok := code.(*Function)
			if !ok {
				t.Fatalf("expected *Function, got %T", code)
			}
			if f.ReturnType != goType {
				t.Errorf("return type = %q, want %q", f.ReturnType, goType)
			}
		})
	}
}

func TestNewCode_AllArrayParams(t *testing.T) {
	types := []string{
		"string", "int16", "uint16", "int32", "uint32",
		"int64", "int", "uint", "float32", "float64", "bool",
	}
	for _, elemType := range types {
		t.Run("[]"+elemType+"_param", func(t *testing.T) {
			fd := parseFuncDecl(t, `package main
func Fn(v []`+elemType+`) {}`)
			code, err := NewCode(fd)
			if err != nil {
				t.Fatal(err)
			}
			vf, ok := code.(*VoidFunction)
			if !ok {
				t.Fatalf("expected *VoidFunction, got %T", code)
			}
			if len(vf.Params) != 1 {
				t.Fatalf("params count = %d, want 1", len(vf.Params))
			}
			if vf.Params[0].Type != "[]"+elemType {
				t.Errorf("param type = %q, want %q", vf.Params[0].Type, "[]"+elemType)
			}
		})
		t.Run("[]"+elemType+"_return", func(t *testing.T) {
			fd := parseFuncDecl(t, `package main
func Fn() []`+elemType+` { return nil }`)
			code, err := NewCode(fd)
			if err != nil {
				t.Fatal(err)
			}
			f, ok := code.(*Function)
			if !ok {
				t.Fatalf("expected *Function, got %T", code)
			}
			if f.ReturnType != "[]"+elemType {
				t.Errorf("return type = %q, want %q", f.ReturnType, "[]"+elemType)
			}
		})
	}
}

func TestNewCode_ByteaParamAndReturn(t *testing.T) {
	fd := parseFuncDecl(t, `package main
func Fn(data []byte) []byte { return data }`)
	code, err := NewCode(fd)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := code.(*Function)
	if !ok {
		t.Fatalf("expected *Function, got %T", code)
	}
	if len(f.Params) != 1 || f.Params[0].Type != "[]byte" {
		t.Errorf("param = %+v, want []byte", f.Params)
	}
	if f.ReturnType != "[]byte" {
		t.Errorf("return type = %q, want []byte", f.ReturnType)
	}
}

func TestNewCode_MultipleParamsSameType(t *testing.T) {
	fd := parseFuncDecl(t, `package main
func Fn(a, b, c string) string { return a+b+c }`)
	code, err := NewCode(fd)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := code.(*Function)
	if !ok {
		t.Fatalf("expected *Function, got %T", code)
	}
	if len(f.Params) != 3 {
		t.Fatalf("params count = %d, want 3", len(f.Params))
	}
	names := []string{"a", "b", "c"}
	for i, p := range f.Params {
		if p.Name != names[i] || p.Type != "string" {
			t.Errorf("param[%d] = %+v, want {%s string}", i, p, names[i])
		}
	}
}

func TestNewCode_MixedParamTypes(t *testing.T) {
	fd := parseFuncDecl(t, `package main
func Fn(name string, age int32, score float64, active bool) {}`)
	code, err := NewCode(fd)
	if err != nil {
		t.Fatal(err)
	}
	vf := code.(*VoidFunction)
	expected := []Param{
		{"name", "string"},
		{"age", "int32"},
		{"score", "float64"},
		{"active", "bool"},
	}
	if len(vf.Params) != len(expected) {
		t.Fatalf("params count = %d, want %d", len(vf.Params), len(expected))
	}
	for i, p := range vf.Params {
		if p != expected[i] {
			t.Errorf("param[%d] = %+v, want %+v", i, p, expected[i])
		}
	}
}

func TestFunction_SQL_MixedParams(t *testing.T) {
	f := &Function{
		VoidFunction: VoidFunction{
			Name: "Fn",
			Params: []Param{
				{"name", "string"},
				{"age", "int32"},
				{"score", "float64"},
				{"active", "bool"},
			},
		},
		ReturnType: "string",
	}
	var buf bytes.Buffer
	f.SQL("ext", &buf)
	sql := buf.String()
	if !strings.Contains(sql, "name text,age integer,score double precision,active boolean") {
		t.Errorf("wrong param list, got:\n%s", sql)
	}
}

func TestVoidFunction_Code_NoParams(t *testing.T) {
	f := &VoidFunction{Name: "Noop"}
	var buf bytes.Buffer
	f.Code(&buf)
	code := buf.String()
	if strings.Contains(code, "fcinfo.Scan") {
		t.Error("should not call fcinfo.Scan when no params")
	}
	if !strings.Contains(code, "__Noop(") {
		t.Error("missing __Noop call")
	}
}

func TestTriggerFunction_Code(t *testing.T) {
	f := &TriggerFunction{VoidFunction: VoidFunction{Name: "MyTrig"}}
	var buf bytes.Buffer
	f.Code(&buf)
	code := buf.String()
	if !strings.Contains(code, "//export MyTrig") {
		t.Errorf("missing //export")
	}
	if !strings.Contains(code, "fcinfo.TriggerData()") {
		t.Errorf("missing TriggerData() call")
	}
	if !strings.Contains(code, "ret := __MyTrig(") {
		t.Errorf("missing __MyTrig call")
	}
	if !strings.Contains(code, "return toDatum(ret)") {
		t.Errorf("missing return toDatum(ret)")
	}
}

func TestDatumTypes_PGTypeCorrectness(t *testing.T) {
	// Verify specific PG type strings are valid SQL type names
	expected := map[string]string{
		"string":      "text",
		"[]byte":      "bytea",
		"int16":       "smallint",
		"int32":       "integer",
		"int64":       "bigint",
		"float32":     "real",
		"float64":     "double precision",
		"bool":        "boolean",
		"time.Time":   "timestamp with timezone",
		"TriggerRow":  "trigger",
		"[]string":    "text[]",
		"[]int32":     "integer[]",
		"[]float64":   "double precision[]",
		"[]bool":      "boolean[]",
		"[]time.Time": "timestamp with timezone[]",
	}
	for goType, wantPG := range expected {
		got, ok := datumTypes[goType]
		if !ok {
			t.Errorf("datumTypes missing %q", goType)
			continue
		}
		if got != wantPG {
			t.Errorf("datumTypes[%q] = %q, want %q", goType, got, wantPG)
		}
	}
}

func TestFunction_FuncDec(t *testing.T) {
	f := &Function{
		VoidFunction: VoidFunction{Name: "ConcatAll"},
		ReturnType:   "string",
	}
	dec := f.FuncDec()
	if dec != "PG_FUNCTION_INFO_V1(ConcatAll);" {
		t.Errorf("FuncDec() = %q", dec)
	}
}

func TestTriggerFunction_FuncDec(t *testing.T) {
	f := &TriggerFunction{VoidFunction: VoidFunction{Name: "MyTrig"}}
	dec := f.FuncDec()
	if dec != "PG_FUNCTION_INFO_V1(MyTrig);" {
		t.Errorf("FuncDec() = %q", dec)
	}
}

func TestNewCode_TriggerDataNotFirst(t *testing.T) {
	fd := parseFuncDecl(t, `package main
import "gitlab.com/microo8/plgo"
func Bad(name string, td *plgo.TriggerData) *plgo.TriggerRow { return nil }`)
	_, err := NewCode(fd)
	if err == nil {
		t.Fatal("expected error when TriggerData is not first param")
	}
	if !strings.Contains(err.Error(), "first parameter") {
		t.Errorf("error = %q, want 'first parameter'", err.Error())
	}
}

func TestRemover_StripsSelectorExpr(t *testing.T) {
	fset := token.NewFileSet()
	src := `package main

import "gitlab.com/microo8/plgo"

func Foo() {
	db, _ := plgo.Open()
	_ = db
}
`
	file, err := parser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Walk(new(Remover), file)

	// After removal, plgo.Open() should become just Open()
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "plgo" {
					t.Error("Remover did not strip plgo selector from call expression")
				}
			}
		}
		return true
	})
}

func TestRemover_StripsStarExpr(t *testing.T) {
	fset := token.NewFileSet()
	src := `package main

import "gitlab.com/microo8/plgo"

func Foo(td *plgo.TriggerData) {}
`
	file, err := parser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Walk(new(Remover), file)

	// After removal, *plgo.TriggerData should become *TriggerData
	ast.Inspect(file, func(n ast.Node) bool {
		if star, ok := n.(*ast.StarExpr); ok {
			if sel, ok := star.X.(*ast.SelectorExpr); ok {
				if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "plgo" {
					t.Error("Remover did not strip plgo from *plgo.TriggerData")
				}
			}
		}
		return true
	})
}

// --- SetOfFunction tests ---

func TestNewCode_SetOfFunction(t *testing.T) {
	fd := parseFuncDecl(t, `package main
import "gitlab.com/microo8/plgo"
func GenStrings() plgo.SetOf[string] { return nil }`)
	code, err := NewCode(fd)
	if err != nil {
		t.Fatal(err)
	}
	sf, ok := code.(*SetOfFunction)
	if !ok {
		t.Fatalf("expected *SetOfFunction, got %T", code)
	}
	if sf.Name != "GenStrings" {
		t.Errorf("name = %q, want GenStrings", sf.Name)
	}
	if sf.ElementType != "string" {
		t.Errorf("element type = %q, want string", sf.ElementType)
	}
	if len(sf.Params) != 0 {
		t.Errorf("params = %v, want empty", sf.Params)
	}
}

func TestNewCode_SetOfWithParams(t *testing.T) {
	fd := parseFuncDecl(t, `package main
import "gitlab.com/microo8/plgo"
func GenN(n int32) plgo.SetOf[int64] { return nil }`)
	code, err := NewCode(fd)
	if err != nil {
		t.Fatal(err)
	}
	sf, ok := code.(*SetOfFunction)
	if !ok {
		t.Fatalf("expected *SetOfFunction, got %T", code)
	}
	if sf.ElementType != "int64" {
		t.Errorf("element type = %q, want int64", sf.ElementType)
	}
	if len(sf.Params) != 1 || sf.Params[0].Type != "int32" {
		t.Errorf("params = %+v, want [{n int32}]", sf.Params)
	}
}

func TestNewCode_SetOfAllScalarTypes(t *testing.T) {
	types := []string{
		"string", "int16", "uint16", "int32", "uint32",
		"int64", "int", "uint", "float32", "float64", "bool",
	}
	for _, goType := range types {
		t.Run(goType, func(t *testing.T) {
			fd := parseFuncDecl(t, `package main
import "gitlab.com/microo8/plgo"
func Fn() plgo.SetOf[`+goType+`] { return nil }`)
			code, err := NewCode(fd)
			if err != nil {
				t.Fatal(err)
			}
			sf, ok := code.(*SetOfFunction)
			if !ok {
				t.Fatalf("expected *SetOfFunction, got %T", code)
			}
			if sf.ElementType != goType {
				t.Errorf("element type = %q, want %q", sf.ElementType, goType)
			}
		})
	}
}

func TestNewCode_SetOfBytea(t *testing.T) {
	fd := parseFuncDecl(t, `package main
import "gitlab.com/microo8/plgo"
func Fn() plgo.SetOf[[]byte] { return nil }`)
	code, err := NewCode(fd)
	if err != nil {
		t.Fatal(err)
	}
	sf, ok := code.(*SetOfFunction)
	if !ok {
		t.Fatalf("expected *SetOfFunction, got %T", code)
	}
	if sf.ElementType != "[]byte" {
		t.Errorf("element type = %q, want []byte", sf.ElementType)
	}
}

func TestNewCode_SetOfUnsupportedType(t *testing.T) {
	fd := parseFuncDecl(t, `package main
import "gitlab.com/microo8/plgo"
type Custom struct{}
func Fn() plgo.SetOf[Custom] { return nil }`)
	_, err := NewCode(fd)
	if err == nil {
		t.Fatal("expected error for unsupported SetOf element type")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("error = %q, want 'not supported'", err.Error())
	}
}

func TestSetOfFunction_SQL(t *testing.T) {
	tests := []struct {
		elemType string
		pgSetOf  string
	}{
		{"string", "SETOF text"},
		{"int16", "SETOF smallint"},
		{"int32", "SETOF integer"},
		{"int64", "SETOF bigint"},
		{"float32", "SETOF real"},
		{"float64", "SETOF double precision"},
		{"bool", "SETOF boolean"},
		{"[]byte", "SETOF bytea"},
	}
	for _, tt := range tests {
		t.Run(tt.elemType, func(t *testing.T) {
			f := &SetOfFunction{
				VoidFunction: VoidFunction{Name: "Fn"},
				ElementType:  tt.elemType,
			}
			var buf bytes.Buffer
			f.SQL("ext", &buf)
			sql := buf.String()
			if !strings.Contains(sql, "RETURNS "+tt.pgSetOf) {
				t.Errorf("expected 'RETURNS %s', got:\n%s", tt.pgSetOf, sql)
			}
		})
	}
}

func TestSetOfFunction_SQL_WithParams(t *testing.T) {
	f := &SetOfFunction{
		VoidFunction: VoidFunction{
			Name:   "GenN",
			Params: []Param{{Name: "n", Type: "int32"}},
		},
		ElementType: "int64",
	}
	var buf bytes.Buffer
	f.SQL("ext", &buf)
	sql := buf.String()
	if !strings.Contains(sql, "n integer") {
		t.Errorf("missing param, got:\n%s", sql)
	}
	if !strings.Contains(sql, "RETURNS SETOF bigint") {
		t.Errorf("missing RETURNS SETOF bigint, got:\n%s", sql)
	}
	if !strings.Contains(sql, "'$libdir/ext', 'GenN'") {
		t.Errorf("missing libdir ref, got:\n%s", sql)
	}
}

func TestSetOfFunction_SQL_Comment(t *testing.T) {
	f := &SetOfFunction{
		VoidFunction: VoidFunction{Name: "Fn", Doc: "Generates stuff"},
		ElementType:  "string",
	}
	var buf bytes.Buffer
	f.SQL("ext", &buf)
	sql := buf.String()
	if !strings.Contains(sql, "COMMENT ON FUNCTION") {
		t.Errorf("missing COMMENT, got:\n%s", sql)
	}
	if !strings.Contains(sql, "Generates stuff") {
		t.Errorf("missing doc string, got:\n%s", sql)
	}
}

func TestSetOfFunction_Code(t *testing.T) {
	f := &SetOfFunction{
		VoidFunction: VoidFunction{
			Name:   "GenNames",
			Params: []Param{{Name: "n", Type: "int32"}},
		},
		ElementType: "string",
	}
	var buf bytes.Buffer
	f.Code(&buf)
	code := buf.String()
	if !strings.Contains(code, "//export GenNames") {
		t.Errorf("missing //export, got:\n%s", code)
	}
	if !strings.Contains(code, "srfIsFirstCall(fcinfo)") {
		t.Errorf("missing srfIsFirstCall, got:\n%s", code)
	}
	if !strings.Contains(code, "var n int32") {
		t.Errorf("missing param declaration, got:\n%s", code)
	}
	if !strings.Contains(code, "fcinfo.Scan(") {
		t.Errorf("missing fcinfo.Scan, got:\n%s", code)
	}
	if !strings.Contains(code, "result := __GenNames(") {
		t.Errorf("missing __GenNames call, got:\n%s", code)
	}
	if !strings.Contains(code, "srfInit(fcinfo, vals)") {
		t.Errorf("missing srfInit, got:\n%s", code)
	}
	if !strings.Contains(code, "return srfNext(fcinfo)") {
		t.Errorf("missing srfNext return, got:\n%s", code)
	}
	if !strings.Contains(code, "elog_error") {
		t.Errorf("missing elog_error handler, got:\n%s", code)
	}
}

func TestSetOfFunction_Code_NoParams(t *testing.T) {
	f := &SetOfFunction{
		VoidFunction: VoidFunction{Name: "AllNames"},
		ElementType:  "string",
	}
	var buf bytes.Buffer
	f.Code(&buf)
	code := buf.String()
	if strings.Contains(code, "fcinfo.Scan") {
		t.Errorf("should not call fcinfo.Scan when no params, got:\n%s", code)
	}
	if !strings.Contains(code, "result := __AllNames(") {
		t.Errorf("missing __AllNames call, got:\n%s", code)
	}
	if !strings.Contains(code, "srfIsFirstCall(fcinfo)") {
		t.Errorf("missing srfIsFirstCall")
	}
}

func TestSetOfFunction_FuncDec(t *testing.T) {
	f := &SetOfFunction{
		VoidFunction: VoidFunction{Name: "GenSeries"},
		ElementType:  "int32",
	}
	dec := f.FuncDec()
	if dec != "PG_FUNCTION_INFO_V1(GenSeries);" {
		t.Errorf("FuncDec() = %q", dec)
	}
}

func TestRemover_StripsSetOfExpr(t *testing.T) {
	fset := token.NewFileSet()
	src := `package main

import "gitlab.com/microo8/plgo"

func Foo() plgo.SetOf[string] { return nil }
`
	file, err := parser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Walk(new(Remover), file)

	// After removal, plgo.SetOf[string] should become SetOf[string]
	ast.Inspect(file, func(n ast.Node) bool {
		if idx, ok := n.(*ast.IndexExpr); ok {
			if sel, ok := idx.X.(*ast.SelectorExpr); ok {
				if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "plgo" {
					t.Error("Remover did not strip plgo from plgo.SetOf[T]")
				}
			}
		}
		return true
	})
}
