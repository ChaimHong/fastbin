package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"strings"
)

type StructInfo struct {
	Name   string
	Fields []*FieldInfo
}

type FieldInfo struct {
	Name    string
	IsArray bool
	TypeInfo
}

type TypeInfo struct {
	Name       string
	Size       int
	SizeFunc   string
	EncodeFunc string
	DecodeFunc string
	Convert    string
}

var typeInfos = map[string]TypeInfo{
	"byte":    {"byte", 1, "", "WriteUint8LE", "ReadUint8LE", ""},
	"int":     {"int", 8, "", "WriteInt64LE", "ReadInt64LE", "int64"},
	"int8":    {"int8", 1, "", "WriteInt8LE", "ReadInt8LE", ""},
	"int16":   {"int16", 2, "", "WriteInt16LE", "ReadInt16LE", ""},
	"int32":   {"int32", 4, "", "WriteInt32LE", "ReadInt32LE", ""},
	"int64":   {"int64", 8, "", "WriteInt64LE", "ReadInt64LE", ""},
	"uint":    {"uint", 8, "", "WriteUint64LE", "ReadUint64LE", "uint64"},
	"uint8":   {"uint8", 1, "", "WriteUint8LE", "ReadUint8LE", ""},
	"uint16":  {"uint16", 2, "", "WriteUint16LE", "ReadUint16LE", ""},
	"uint32":  {"uint32", 4, "", "WriteUint32LE", "ReadUint32LE", ""},
	"uint64":  {"uint64", 8, "", "WriteUint64LE", "ReadUint64LE", ""},
	"float32": {"float32", 4, "", "WriteFloat32LE", "ReadFloat32LE", ""},
	"float64": {"float64", 8, "", "WriteFloat64LE", "ReadFloat64LE", ""},
	"string":  {"string", 0, "", "", "", ""},
	"varint":  {"varint", 0, "", "", "", ""},
	"Varint":  {"varint", 0, "", "", "", ""},
	"uvarint": {"uvarint", 0, "", "", "", ""},
	"Uvarint": {"uvarint", 0, "", "", "", ""},
}

func main() {
	flag.Parse()
	var filename string
	if len(flag.Args()) > 0 {
		filename = flag.Arg(0)
	} else {
		filename = os.Getenv("GOFILE")
	}
	generateFile(filename)
}

func generateFile(filename string) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		log.Fatalf("Could't parse file '%s': %s", filename, err)
	}

	packageName := f.Name.String()

	var bf bytes.Buffer

	bf.WriteString("package ")
	bf.WriteString(packageName)
	bf.WriteString("\n\n")
	bf.WriteString("import (\n")
	bf.WriteString("	\"github.com/funny/binary\"\n")
	bf.WriteString(")\n")
	bf.WriteString("\n\n")

	for _, d := range f.Scope.Objects {
		if d.Kind == ast.Typ {
			ts, ok := d.Decl.(*ast.TypeSpec)
			if !ok {
				log.Fatalf("Unknown type without TypeSec: %v", d)
			}
			if st, ok := ts.Type.(*ast.StructType); ok {
				generateStruct(&bf, ts.Name.String(), st)
			}
		}
	}

	if len(flag.Args()) == 0 {
		filename = strings.Replace(filename, ".go", ".fast.go", 1)
		file, err := os.Create(filename)
		if err != nil {
			fmt.Errorf("Could't create file '%s': %s", filename, err)
			os.Exit(-1)
		}
		if _, err := file.Write(bf.Bytes()); err != nil {
			fmt.Errorf("Write file '%s' failed: %s", filename, err)
			os.Exit(-1)
		}
		file.Close()
	} else {
		fmt.Print(bf.String())
	}
}

func analyzeStruct(structName string, st *ast.StructType) *StructInfo {
	var si StructInfo
	si.Name = structName
	for _, field := range st.Fields.List {
		var fi FieldInfo

		var ft *ast.Ident
		switch field.Type.(type) {
		case *ast.Ident:
			ft = field.Type.(*ast.Ident)
		case *ast.StarExpr:
			ft = field.Type.(*ast.StarExpr).X.(*ast.Ident)
		case *ast.ArrayType:
			fi.IsArray = true
			switch at := field.Type.(*ast.ArrayType).Elt.(type) {
			case *ast.Ident:
				ft = at
			case *ast.StarExpr:
				ft = at.X.(*ast.Ident)
			}
		}

		fi.Name = field.Names[0].String()
		ti, exists := typeInfos[ft.Name]
		if !exists {
			ti = TypeInfo{Name: ft.Name}
		}
		fi.TypeInfo = ti
		si.Fields = append(si.Fields, &fi)
	}
	return &si
}

func generateStruct(buf *bytes.Buffer, structName string, st *ast.StructType) {
	si := analyzeStruct(structName, st)

	fmt.Fprintf(buf, "func (s *%s) BinarySize() (n int) {\n", si.Name)
	buf.WriteString("	n = 0")
	for _, field := range si.Fields {
		if !field.IsArray {
			switch {
			case field.Size != 0:
				fmt.Fprintf(buf, " + %d", field.Size)
			case field.TypeInfo.Name == "string":
				fmt.Fprintf(buf, " + len(s.%s)", field.Name)
			case field.TypeInfo.Name == "varint":
				fmt.Fprintf(buf, " + int(binary.VarintSize(int64(s.%s)))", field.Name)
			case field.TypeInfo.Name == "uvarint":
				fmt.Fprintf(buf, " + int(binary.UvarintSize(uint64(s.%s)))", field.Name)
			default:
				fmt.Fprintf(buf, " + s.%s.BinarySize()", field.Name)
			}
		} else if field.Size != 0 {
			switch {
			case field.TypeInfo.Name == "byte":
				fmt.Fprintf(buf, " + len(s.%s)", field.Name)
			default:
				fmt.Fprintf(buf, " + len(s.%s) * %d", field.Name, field.Size)
			}
		}
	}
	for _, field := range si.Fields {
		if field.IsArray && field.Size == 0 {
			buf.WriteString("	\n")
			fmt.Fprintf(buf, "	for i := 0; i < len(s.%s); i ++ {\n", field.Name)
			switch {
			case field.TypeInfo.Name == "varint":
				fmt.Fprintf(buf, "		n += int(binary.VarintSize(int64(s.%s[i])))", field.Name)
			case field.TypeInfo.Name == "uvarint":
				fmt.Fprintf(buf, "		n += int(binary.UvarintSize(uint64(s.%s[i])))", field.Name)
			default:
				fmt.Fprintf(buf, "		n += s.%s[i].BinarySize()\n", field.Name)
			}
			buf.WriteString("	}")
		}
	}
	buf.WriteString("\n")
	buf.WriteString("	return\n")
	buf.WriteString("}\n\n")

	fmt.Fprintf(buf, "func (s *%s) MarshalBinary() (data []byte, err error) {\n", si.Name)
	buf.WriteString("	data = make([]byte, s.BinarySize())\n")
	buf.WriteString("	s.MarshalBuffer(&binary.Buffer{Data:data})\n")
	buf.WriteString("	return\n")
	buf.WriteString("}\n\n")

	fmt.Fprintf(buf, "func (s *%s) UnmarshalBinary(data []byte) error {\n", si.Name)
	buf.WriteString("	s.UnmarshalBuffer(&binary.Buffer{Data:data})\n")
	buf.WriteString("	return nil\n")
	buf.WriteString("}\n\n")

	needLenVar := false

	fmt.Fprintf(buf, "func (s *%s) MarshalBuffer(buf *binary.Buffer) {\n", si.Name)
	for _, field := range si.Fields {
		if !field.IsArray {
			switch {
			case field.TypeInfo.Size != 0:
				if field.Convert == "" {
					fmt.Fprintf(buf, "	buf.%s(s.%s)\n", field.EncodeFunc, field.Name)
				} else {
					fmt.Fprintf(buf, "	buf.%s(%s(s.%s))\n", field.EncodeFunc, field.Convert, field.Name)
				}
			case field.TypeInfo.Name == "string":
				fmt.Fprintf(buf, "	buf.WriteUint16LE(uint16(len(s.%s)))\n", field.Name)
				fmt.Fprintf(buf, "	buf.WriteString(s.%s)\n", field.Name)
			case field.TypeInfo.Name == "varint":
				fmt.Fprintf(buf, "	buf.WriteVarint(int64(s.%s))\n", field.Name)
			case field.TypeInfo.Name == "uvarint":
				fmt.Fprintf(buf, "	buf.WriteUvarint(uint64(s.%s))\n", field.Name)
			default:
				fmt.Fprintf(buf, "	s.%s.MarshalBuffer(buf)\n", field.Name)
			}
		} else {
			fmt.Fprintf(buf, "	buf.WriteUint16LE(uint16(len(s.%s)))\n", field.Name)
			switch {
			case field.TypeInfo.Name == "byte":
				fmt.Fprintf(buf, "	buf.WriteBytes(s.%s)\n", field.Name)
			case field.TypeInfo.Size != 0:
				needLenVar = true
				fmt.Fprintf(buf, "	for i := 0; i < len(s.%s); i ++ {\n", field.Name)
				if field.Convert == "" {
					fmt.Fprintf(buf, "		buf.%s(s.%s[i])\n", field.EncodeFunc, field.Name)
				} else {
					fmt.Fprintf(buf, "		buf.%s(%s(s.%s[i]))\n", field.EncodeFunc, field.Convert, field.Name)
				}
				buf.WriteString("	}\n")
			default:
				needLenVar = true
				fmt.Fprintf(buf, "	for i := 0; i < len(s.%s); i ++ {\n", field.Name)
				switch {
				case field.TypeInfo.Name == "string":
					fmt.Fprintf(buf, "		buf.WriteUint16LE(uint16(len(s.%s[i])))\n", field.Name)
					fmt.Fprintf(buf, "		buf.WriteString(s.%s[i])\n", field.Name)
				case field.TypeInfo.Name == "varint":
					fmt.Fprintf(buf, "		buf.WriteVarint(int64(s.%s[i]))\n", field.Name)
				case field.TypeInfo.Name == "uvarint":
					fmt.Fprintf(buf, "		buf.WriteUvarint(uint64(s.%s[i]))\n", field.Name)
				default:
					fmt.Fprintf(buf, "		s.%s[i].MarshalBuffer(buf)\n", field.Name)
				}
				buf.WriteString("	}\n")
			}
		}
	}
	buf.WriteString("}\n\n")

	fmt.Fprintf(buf, "func (s *%s) UnmarshalBuffer(buf *binary.Buffer) {\n", si.Name)
	if needLenVar {
		buf.WriteString("	n := 0\n")
	}
	for _, field := range si.Fields {
		if !field.IsArray {
			switch {
			case field.TypeInfo.Size != 0:
				if field.Convert == "" {
					fmt.Fprintf(buf, "	s.%s = buf.%s()\n", field.Name, field.DecodeFunc)
				} else {
					fmt.Fprintf(buf, "	s.%s = %s(buf.%s())\n", field.Name, field.TypeInfo.Name, field.DecodeFunc)
				}
			case field.TypeInfo.Name == "string":
				fmt.Fprintf(buf, "	s.%s = buf.ReadString(int(buf.ReadUint16LE()))\n", field.Name)
			case field.TypeInfo.Name == "varint" || field.TypeInfo.Name == "Varint":
				fmt.Fprintf(buf, "	s.%s = %s(buf.ReadVarint())\n", field.Name, field.TypeInfo.Name)
			case field.TypeInfo.Name == "uvarint" || field.TypeInfo.Name == "Uvarint":
				fmt.Fprintf(buf, "	s.%s = %s(buf.ReadUvarint())\n", field.Name, field.TypeInfo.Name)
			default:
				fmt.Fprintf(buf, "	s.%s.UnmarshalBuffer(buf)\n", field.Name)
			}
		} else {
			switch {
			case field.TypeInfo.Name == "byte":
				fmt.Fprintf(buf, "	s.%s = buf.ReadBytes(int(buf.ReadUint16LE()))\n", field.Name)
			case field.Size != 0:
				buf.WriteString("	n = int(buf.ReadUint16LE())\n")
				buf.WriteString("	for i := 0; i < n; i ++ {\n")
				if field.Convert == "" {
					fmt.Fprintf(buf, "		s.%s[i] = buf.%s()\n", field.Name, field.DecodeFunc)
				} else {
					fmt.Fprintf(buf, "		s.%s[i] = %s(buf.%s())\n", field.Name, field.TypeInfo.Name, field.DecodeFunc)
				}
				buf.WriteString("	}\n")
			default:
				buf.WriteString("	n = int(buf.ReadUint16LE())\n")
				buf.WriteString("	for i := 0; i < n; i ++ {\n")
				switch {
				case field.TypeInfo.Name == "string":
					fmt.Fprintf(buf, "		s.%s[i] = buf.ReadString(int(buf.ReadUint16LE()))\n", field.Name)
				case field.TypeInfo.Name == "varint" || field.TypeInfo.Name == "Varint":
					fmt.Fprintf(buf, "		s.%s[i] = %s(buf.ReadVarint())\n", field.Name, field.TypeInfo.Name)
				case field.TypeInfo.Name == "uvarint" || field.TypeInfo.Name == "Uvarint":
					fmt.Fprintf(buf, "		s.%s[i] = %s(buf.ReadUvarint())\n", field.Name, field.TypeInfo.Name)
				default:
					fmt.Fprintf(buf, "		s.%s[i].UnmarshalBuffer(buf)\n", field.Name)
				}
				buf.WriteString("	}\n")
			}
		}
	}
	buf.WriteString("}\n\n")
}
