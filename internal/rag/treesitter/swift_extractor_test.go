package treesitter

import (
	"strings"
	"testing"
)

const swiftFixture = `import Foundation
import struct Foo.Bar

protocol Shape: Sendable {
    associatedtype Unit
    var area: Double { get }
    func scale(by factor: Double) -> Self
    init(side: Double)
}

struct Square: Shape {
    let side: Double
    var label: String = "sq"

    init(side: Double) {
        self.side = side
    }

    subscript(index: Int) -> Double {
        return side
    }

    var area: Double {
        return side * side
    }

    func scale(by factor: Double) -> Square {
        return Square(side: side * factor)
    }
}

enum Color {
    case red
    case green, blue
    case custom(Int)
}

class Base {
    var count = 0

    init() {
        count = 1
    }

    deinit {
        print("bye")
    }
}

actor Counter {
    var value = 0

    func increment() {
        value += 1
    }
}

typealias Pair = (Int, Int)

extension Square: CustomStringConvertible {
    var description: String {
        return "Square"
    }

    func double() -> Square {
        return scale(by: 2)
    }
}

func topLevel() {
    print("x")
}
`

func swiftSymbolMap(t *testing.T) map[string]*CodeSymbol {
	t.Helper()
	walker := NewASTWalker(DefaultWalkerConfig())
	parser := NewParser()
	defer parser.Close()
	symbols, _ := parseAndExtract(t, walker, parser, LanguageSwift, "a.swift", swiftFixture)
	m := map[string]*CodeSymbol{}
	for _, s := range symbols {
		m[strings.TrimPrefix(strings.ReplaceAll(s.NamePath, "/", "."), ".")] = s
	}
	return m
}

func TestSwiftExtractorSymbols(t *testing.T) {
	m := swiftSymbolMap(t)
	want := map[string]SymbolType{
		"Shape":              SymbolTypeInterface,
		"Shape.Unit":         SymbolTypeTypeAlias,
		"Shape.area":         SymbolTypeProperty,
		"Shape.scale":        SymbolTypeMethod,
		"Shape.init":         SymbolTypeConstructor,
		"Square":             SymbolTypeStruct,
		"Square.side":        SymbolTypeConstant,
		"Square.label":       SymbolTypeProperty,
		"Square.init":        SymbolTypeConstructor,
		"Square.subscript":   SymbolTypeMethod,
		"Square.area":        SymbolTypeProperty,
		"Square.scale":       SymbolTypeMethod,
		"Color":              SymbolTypeEnum,
		"Color.red":          SymbolTypeEnumMember,
		"Color.green":        SymbolTypeEnumMember,
		"Color.blue":         SymbolTypeEnumMember,
		"Color.custom":       SymbolTypeEnumMember,
		"Base":               SymbolTypeClass,
		"Base.init":          SymbolTypeConstructor,
		"Base.deinit":        SymbolTypeMethod,
		"Counter":            SymbolTypeClass,
		"Counter.increment":  SymbolTypeMethod,
		"Pair":               SymbolTypeTypeAlias,
		"topLevel":           SymbolTypeFunction,
		"Square.description": SymbolTypeProperty,
		"Square.double":      SymbolTypeMethod,
	}
	for path, st := range want {
		got, ok := m[path]
		if !ok {
			t.Errorf("missing symbol %q", path)
			continue
		}
		if got.SymbolType != st {
			t.Errorf("%s: type = %s, want %s", path, got.SymbolType, st)
		}
	}
	if m["Counter"] != nil && m["Counter"].Metadata["kind"] != "actor" {
		t.Errorf("Counter kind = %v, want actor", m["Counter"].Metadata["kind"])
	}
	if m["Square"] != nil && m["Square"].Metadata["kind"] != "struct" {
		t.Errorf("Square kind = %v", m["Square"].Metadata["kind"])
	}
	// Extension: members tagged, no duplicate type symbol.
	if d := m["Square.double"]; d != nil && d.Metadata["extension_of"] != "Square" {
		t.Errorf("extension_of = %v", d.Metadata["extension_of"])
	}
	if d := m["Square.description"]; d != nil {
		conf, _ := d.Metadata["conforms_to"].([]string)
		if len(conf) != 1 || conf[0] != "CustomStringConvertible" {
			t.Errorf("conforms_to = %v", d.Metadata["conforms_to"])
		}
	}
	n := 0
	walker := NewASTWalker(DefaultWalkerConfig())
	parser := NewParser()
	defer parser.Close()
	symbols, _ := parseAndExtract(t, walker, parser, LanguageSwift, "a.swift", swiftFixture)
	for _, s := range symbols {
		if s.NamePath == "/Square" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected exactly one Square symbol, got %d", n)
	}
}
