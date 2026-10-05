package treesitter

import "testing"

const kotlinEdgesFixture = `package com.example.app

import kotlin.collections.List
import java.util.*
import com.foo.Bar as Baz

interface Shape {
    fun area(): Double
}

enum class Color { RED, GREEN }

data class Point(val x: Int, val y: Int) : Base(1), Shape, Comparable<Point> {
    companion object Factory {
        const val MAX = 1
    }
    val items: Map<String, Item> = mapOf()
    constructor(a: Int) : this(a, 0)
    fun ext(): List<Foo> {
        return helper(1).bar.baz(2)
    }
}

object Single : Runner {
}

fun String.shout(c: Config): Result<Out> {
    val s = Widget(1)
    println(s)
    return s
}

typealias Alias = Map<String, Int>
`

func TestKotlinEdges(t *testing.T) {
	walker := NewASTWalker(DefaultWalkerConfig())
	parser := NewParser()
	defer parser.Close()
	symbols, edges := parseAndExtract(t, walker, parser, LanguageKotlin, "a.kt", kotlinEdgesFixture)

	for _, p := range []string{"kotlin.collections.List", "java.util.*", "com.foo.Bar"} {
		if !hasImport(edges, p) {
			t.Errorf("missing import %q: %+v", p, edgesByType(edges, EdgeImports))
		}
	}
	for _, c := range []string{"mapOf", "helper", "baz", "println", "Widget", "Base"} {
		if !hasCall(edges, c) {
			t.Errorf("missing call %q: %+v", c, edgesByType(edges, EdgeCalls))
		}
	}
	if hasCall(edges, "bar") {
		t.Error("property access (no call) must not be a call edge")
	}
	for _, ty := range []string{"Int", "Shape", "Comparable", "Point", "Map", "String", "Item", "List", "Foo", "Config", "Result", "Out", "Runner", "Double", "Base"} {
		if !hasEdge(edges, EdgeTypeRef, ty) {
			t.Errorf("missing type_ref %q", ty)
		}
	}
	// Declaration names, import aliases must not be type refs from their own header.
	for _, e := range edgesByType(edges, EdgeTypeRef) {
		if e.DstName == "Baz" || e.DstName == "Factory" || e.DstName == "Alias" || e.DstName == "Color" {
			t.Errorf("declaration/alias name leaked as type_ref: %+v", e)
		}
	}

	var shout, ext string
	for _, s := range symbols {
		switch s.Name {
		case "shout":
			shout = s.ID
		case "ext":
			ext = s.ID
		}
	}
	for _, e := range edgesByType(edges, EdgeCalls) {
		if e.DstName == "println" && e.SrcSymbolID != shout {
			t.Errorf("println not attributed to shout: %+v", e)
		}
		if e.DstName == "helper" && e.SrcSymbolID != ext {
			t.Errorf("helper not attributed to ext: %+v", e)
		}
	}
}

func TestKotlinSymbolKinds(t *testing.T) {
	walker := NewASTWalker(DefaultWalkerConfig())
	parser := NewParser()
	defer parser.Close()
	symbols, _ := parseAndExtract(t, walker, parser, LanguageKotlin, "a.kt", kotlinEdgesFixture)
	want := map[string]SymbolType{
		"Shape":   SymbolTypeInterface,
		"Color":   SymbolTypeEnum,
		"RED":     SymbolTypeEnumMember,
		"Point":   SymbolTypeClass,
		"Factory": SymbolTypeClass,
		"MAX":     SymbolTypeConstant,
		"items":   SymbolTypeProperty,
		"ext":     SymbolTypeMethod,
		"area":    SymbolTypeMethod,
		"shout":   SymbolTypeFunction,
		"Alias":   SymbolTypeTypeAlias,
		"Single":  SymbolTypeClass,
	}
	got := map[string]SymbolType{}
	for _, s := range symbols {
		got[s.Name] = s.SymbolType
	}
	for n, ty := range want {
		if got[n] != ty {
			t.Errorf("symbol %s: got %q want %q", n, got[n], ty)
		}
	}
}
