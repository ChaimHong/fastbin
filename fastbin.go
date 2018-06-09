package fastbin

import (
	"encoding/json"
	gotypes "go/types"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

import (
	"github.com/ChaimHong/ReflectType2GoType"
	"github.com/ChaimHong/gobuf/parser"
	"github.com/ChaimHong/gobuf/plugins/go"
)

var types []reflect.Type

func Register(v interface{}) {
	RegisterType(reflect.TypeOf(v))
}

func RegisterType(t reflect.Type) {
	for i := 0; i < len(types); i++ {
		if t == types[i] {
			return
		}
	}
	types = append(types, t)
}

func Types() []reflect.Type {
	return types
}

func GenCode() {
	a := newAnalyzer()
	a.Analyze(types)

	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		panic("GOPATH environment variable missing")
	}

	path, err := filepath.Abs(gopath)
	if err != nil {
		panic(err)
	}
	path = filepath.Join(path, "src")

	newConver := rtype2gtype.NewConver()

	type pkgHadInfo struct {
		had  bool
		path string
	}
	for _, pkg := range a.Packages {
		structs := map[string]*gotypes.Struct{}
		hadPkgs := map[string]*pkgHadInfo{}

		for tk, ts := range pkg.Types {
			pkgs := []string{}
			typeStruct, pkgs := newConver.Conver(ts.Type)
			structs[tk] = typeStruct.(*gotypes.Struct)

			// IMPORTS
			for _, s0 := range pkgs {
				pkgArr := strings.Split(s0, "/")
				hadPkgs[pkgArr[len(pkgArr)-1]] = &pkgHadInfo{
					had:  false,
					path: s0,
				}
			}
		}

		doc, err := parser.ParseData(pkg.PkgName, nil, structs, nil)
		if err != nil {
			panic(err)
		}

		// fix otherpkgs
		// TODO use parser
		for _, structV := range doc.Structs {
			for _, field := range structV.Fields {
				t := field.Type
				typeName := field.Type.Name
				for typeName == "" {
					t = t.Elem
					if t == nil {
						break
					}

					typeName = t.Name
				}

				var checkname = getCheckStructName(field.Type, 0)
				typeNameArr := strings.Split(checkname, ".")
				if len(typeNameArr) < 2 {
					continue
				}
				hadPkgs[typeNameArr[0]].had = true

			}
		}
		newPkgs := []string{}
		for _, v := range hadPkgs {
			if v.had {
				newPkgs = append(newPkgs, v.path)
			}
		}
		doc.OtherPkg = newPkgs

		jsonData, err2 := json.MarshalIndent(doc, "", " ")

		if err2 != nil {
			panic(err2)
		}

		log.Println(path, pkg.Path)

		if code, err := gosource.Gen(jsonData, false); err == nil {
			saveCode(
				filepath.Join(path, pkg.Path),
				filepath.Base(pkg.Path)+".fastbin.go",
				code,
			)
		} else {
			panic(err)
		}
	}
}

func getCheckStructName(typ *parser.Type, level int8) string {

	level++
	switch typ.Kind {
	case parser.ARRAY, parser.MAP:
		return getCheckStructName(typ.Elem, level)
	case parser.POINTER:
		return typ.Elem.Name
	case parser.INT, parser.UINT, parser.INT8, parser.UINT8, parser.INT16, parser.UINT16, parser.INT32,
		parser.UINT32, parser.INT64, parser.UINT64, parser.FLOAT32, parser.FLOAT64, parser.BOOL:
		return typ.Name
	}

	if level == 1 {
		return ""
	}
	return typ.Name
}

func saveCode(dir, filename string, code []byte) {
	filename = filepath.Join(dir, filename)
	file, err := os.Create(filename)
	if err != nil {
		log.Fatalf("Create file '%s' failed: %s", filename, err)
	}
	if _, err := file.Write(code); err != nil {
		log.Fatalf("Write file '%s' failed: %s", filename, err)
	}
	file.Close()
}
