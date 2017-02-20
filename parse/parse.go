package parse

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"strings"

	"os"

	log "github.com/Sirupsen/logrus"
	"github.com/natdm/typewriter/template"
)

var (
	errSkipType           = errors.New("not a supported type")
	errTypeAssert         = errors.New("type assertion failed")
	errParsingTypeDetails = errors.New("failed to parse type within type")
	errEmbeddedType       = errors.New("embedded type")
)

// commentFlags are flags declared in package-level types to be handed down the parsing logic
type commentFlags struct {
	// strict is for flow types only.
	strict bool

	// ignore ignores the type from being parsed
	ignore bool
}

// Directory parses a directory and returns all the go files that are not test files
// It takes a directory, a recursive boolean option, and an out to put the files in.
func Directory(d string, r bool, out *[]string, verbose bool) error {
	fs, err := ioutil.ReadDir(d)
	if err != nil {
		return err
	}
	for _, v := range fs {
		name := v.Name()
		if v.IsDir() {
			if r {
				if err := Directory(d+"/"+name, r, out, verbose); err != nil {
					return err
				}
			}
		} else if strings.HasSuffix(name, "go") && !strings.Contains(name, "_test.go") {
			*out = append(*out, strings.Replace(fmt.Sprintf("%s/%s", d, name), "//", "/", -1))
		}
	}
	return nil
}

// findImports should keep either the alias name of the import or the package name of an imported package
func findImports(f *ast.File) map[string]string {
	r := strings.NewReplacer("\"", "")
	imports := make(map[string]string)
	for _, v := range f.Imports {
		srcpath := os.Getenv("GOPATH") + "/src/"
		if v.Name != nil {
			imports[v.Name.String()] = r.Replace(srcpath + v.Path.Value)
		} else {
			_n := strings.Split(v.Path.Value, "/")
			n := r.Replace(_n[len(_n)-1])
			imports[n] = r.Replace(srcpath + v.Path.Value)
		}
	}
	return imports
}

// Files parses files and returns the type information
func Files(files []string, verbose bool) (map[string]*template.PackageType, error) {
	typs := make(map[string]*template.PackageType)
	for _, name := range files {
		fset := token.NewFileSet() // positions are relative to fset

		// Parse the file given in arguments
		f, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}

		imports := findImports(f)
		log.Println(imports)

		comments := make(map[string]string)
		for _, v := range f.Comments {
			c := v.Text()
			comments[firstWord(c)] = c
		}

		bs, err := ioutil.ReadFile(name)
		if err != nil {
			return nil, err
		}

	OBJLOOP:
		for _, v := range f.Scope.Objects {
			if v.Kind == ast.Typ {
				comment := comments[v.Name]
				flags := commentFlags{
					strict: strings.Contains(comment, "@strict"),
					ignore: strings.Contains(comment, "@ignore"),
				}
				if flags.ignore {
					if verbose {
						log.WithField("type_name", v.Name).WithField("file_name", name).Info("skipping type with '@ignore' flag")
					}
					continue
				}
				ts, ok := v.Decl.(*ast.TypeSpec)
				if !ok {
					continue OBJLOOP
				}
				t, err := Type(bs, ts, verbose, flags)
				if err != nil {
					if verbose {
						log.WithError(err).WithField("type_name", v.Name).WithField("file_name", name).Error("error parsing type, skipped")
					}
					continue OBJLOOP
				}
				t.Comment = comment
				typs[v.Name] = t
			}
		}
	}

	parseEmbedded(typs)
	return typs, nil
}

// parseEmbedded nests embedded type fields in the structs containing embedded types
func parseEmbedded(types map[string]*template.PackageType) {
	for _, v := range types {
		if s, ok := v.Type.(*template.Struct); ok {

			// If any of the structs have embedded types, transfer them to the struct
			if len(s.Embedded) > 0 {
			EMBEDLOOP:
				for _, v := range s.Embedded {
					_v := strings.TrimSpace(v)
					if _, ok := types[_v]; ok {
						if s2, ok := types[_v].Type.(*template.Struct); ok {
							s.Fields = append(s.Fields, s2.Fields...)
							continue EMBEDLOOP
						}
						continue EMBEDLOOP
					}
				}
			}
		}
	}
}

// Type creates a package level type.
func Type(bs []byte, ts *ast.TypeSpec, verbose bool, flags commentFlags) (*template.PackageType, error) {
	s := &template.PackageType{}
	s.Name = ts.Name.Name
	if ts.Comment != nil {
		s.Comment = ts.Comment.Text()
	}

	switch ts.Type.(type) {
	case *ast.ChanType, *ast.FuncLit, *ast.FuncType:
		return nil, errSkipType

	case *ast.InterfaceType:
		return nil, errSkipType

	case *ast.ArrayType:
		x, ok := ts.Type.(*ast.ArrayType)
		if !ok {
			return nil, errTypeAssert
		}
		t, err := parseType(x.Elt)
		if err != nil {
			return nil, err
		}
		s.Type = &template.Array{
			Type: t,
		}
		return s, nil

	case *ast.MapType:
		x, ok := ts.Type.(*ast.MapType)
		if !ok {
			return nil, errTypeAssert
		}
		key, err := parseType(x.Key)
		if err != nil {
			return nil, err
		}
		val, err := parseType(x.Value)
		if err != nil {
			return nil, err
		}
		s.Type = &template.Map{
			Key:   key,
			Value: val,
		}
		return s, nil

	case *ast.StructType:
		x, ok := ts.Type.(*ast.StructType)
		if !ok {
			return nil, errTypeAssert
		}
		str := &template.Struct{}
		str.Strict = flags.strict
	FIELDLOOP:
		for _, v := range x.Fields.List {
			typ, err := parseType(v.Type)
			if err != nil {
				log.Println(err)
				continue FIELDLOOP
			}
			if v.Names == nil {
				// No names on a type means it is embedded

				str.Embedded = append(str.Embedded, string(bs[v.Type.Pos()-2:v.Type.End()-1]))
				log.Printf("found embedded type: %s\n", string(bs[v.Type.Pos()-2:v.Type.End()-1]))
				continue FIELDLOOP
			}
			if v.Names[0] == nil {
				continue FIELDLOOP
			}
			fld := template.Field{}
			fld.Type = typ
			if v.Comment != nil {
				fld.Comment = v.Comment.Text()
			}
			if v.Tag == nil || strings.Contains(v.Tag.Value, "json:\"-\"") {
				continue FIELDLOOP
			}
			fld.Tag = v.Tag.Value
			fld.Name = v.Names[0].Name
			str.Fields = append(str.Fields, fld)
		}
		s.Type = str
		return s, nil

	default:
		t := inspectNode(ts)
		s.Type = &t
		return s, nil

	}
}

func findEmenddedType(packages []string, name string) {

}

// parseType parses a non-package level type.
func parseType(exp ast.Expr) (template.Templater, error) {
	switch exp.(type) {
	case *ast.ChanType, *ast.FuncLit, *ast.FuncType:
		return nil, errSkipType

	case *ast.InterfaceType:
		x, ok := exp.(*ast.InterfaceType)
		if !ok {
			return nil, errTypeAssert
		}
		// Empty interface should be the closes to "any" that we can
		// get in any language
		if x.Methods != nil && x.Methods.NumFields() == 0 {
			return &template.Basic{
				Type:    template.EmptyInterface,
				Pointer: true,
			}, nil
		}
		return nil, errSkipType

	case *ast.ArrayType:
		x, ok := exp.(*ast.ArrayType)
		if !ok {
			return nil, errTypeAssert
		}
		t, err := parseType(x.Elt)
		if err != nil {
			return nil, err
		}
		return &template.Array{Type: t}, nil

	case *ast.MapType:
		x, ok := exp.(*ast.MapType)
		if !ok {
			return nil, errTypeAssert
		}
		key, err := parseType(x.Key)
		if err != nil {
			return nil, err
		}
		val, err := parseType(x.Value)
		if err != nil {
			return nil, err
		}
		return &template.Map{
			Key:   key,
			Value: val,
		}, nil

	case *ast.StructType:
		return &template.Basic{
			Type:    template.NestedStruct,
			Pointer: false,
		}, nil

	case *ast.BasicLit:
		x, ok := exp.(*ast.BasicLit)
		if !ok {
			return nil, errTypeAssert
		}
		return &template.Basic{
			Type:    x.Kind.String(),
			Pointer: false,
		}, nil

	default:
		t := inspectNode(exp)
		return &t, nil
	}
}

// inspectNode checks what is determined to be the value of a node based on a type-assertion.
func inspectNode(node ast.Node) template.Basic {
	var t template.Basic
	ast.Inspect(node, func(n ast.Node) bool {
		switch y := n.(type) {
		case *ast.BasicLit:
			t.Type = y.Value
		case *ast.Ident:
			t.Type = y.Name
		case *ast.StarExpr:
			t.Pointer = true
		}
		return true
	})
	return t
}

// first word returns the first word of a string
func firstWord(value string) string {
	for i := range value {
		if value[i] == ' ' {
			return value[0:i]
		}
	}
	return value
}
