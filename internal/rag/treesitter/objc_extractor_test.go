package treesitter

import "testing"

func symByName(syms []*CodeSymbol, name string, st SymbolType) *CodeSymbol {
	for _, s := range syms {
		if s.Name == name && s.SymbolType == st {
			return s
		}
	}
	return nil
}

const objcSrc = `#import <Foundation/Foundation.h>
#import "Foo.h"
@import UIKit;
@class Bar;
@protocol Greeter <NSObject>
- (void)greet:(NSString *)name;
@end
@interface Foo : NSObject <Greeter, NSCopying>
@property (nonatomic, strong) NSString *name;
@property int count;
+ (instancetype)sharedFoo;
- (id)initWithFoo:(int)a bar:(NSString *)b;
- (void)run;
@end
@interface Foo (Extras)
- (void)extra;
@end
@implementation Foo
+ (instancetype)sharedFoo { return [[Foo alloc] init]; }
- (id)initWithFoo:(int)a bar:(NSString *)b { self = [super init]; [self setX:1 withY:2]; helper(a); return self; }
- (void)run { NSLog(@"x"); }
@end
typedef struct Pt { int x; } Pt;
enum Color { Red, Green };
static int helper(int a) { return a; }
`

func TestObjCExtractor(t *testing.T) {
	walker := NewASTWalker(DefaultWalkerConfig())
	parser := NewParser()
	defer parser.Close()
	syms, edges := parseAndExtract(t, walker, parser, LanguageObjectiveC, "Foo.m", objcSrc)

	want := []struct {
		name string
		st   SymbolType
	}{
		{"Greeter", SymbolTypeInterface},
		{"Foo", SymbolTypeClass},
		{"Foo(Extras)", SymbolTypeClass},
		{"greet:", SymbolTypeMethod},
		{"sharedFoo", SymbolTypeMethod},
		{"initWithFoo:bar:", SymbolTypeConstructor},
		{"run", SymbolTypeMethod},
		{"extra", SymbolTypeMethod},
		{"name", SymbolTypeProperty},
		{"count", SymbolTypeProperty},
		{"helper", SymbolTypeFunction},
		{"Pt", SymbolTypeTypeAlias},
		{"Color", SymbolTypeEnum},
	}
	for _, w := range want {
		if symByName(syms, w.name, w.st) == nil {
			t.Errorf("missing %s %q", w.st, w.name)
		}
	}
	for _, s := range syms {
		if s.Language != LanguageObjectiveC {
			t.Errorf("symbol %q has language %s", s.Name, s.Language)
		}
	}

	for _, p := range []string{"Foundation/Foundation.h", "Foo.h", "UIKit"} {
		if !hasImport(edges, p) {
			t.Errorf("missing import %q: %+v", p, edgesByType(edges, EdgeImports))
		}
	}
	for _, c := range []string{"alloc", "init", "setX:withY:", "helper", "NSLog"} {
		if !hasEdge(edges, EdgeCalls, c) {
			t.Errorf("missing call %q: %+v", c, edgesByType(edges, EdgeCalls))
		}
	}
	for _, r := range []string{"NSObject", "Greeter", "NSCopying", "Bar", "Foo"} {
		if !hasEdge(edges, EdgeTypeRef, r) {
			t.Errorf("missing type_ref %q: %+v", r, edgesByType(edges, EdgeTypeRef))
		}
	}
	if hasEdge(edges, EdgeTypeRef, "Pt") && len(edgesByType(edges, EdgeTypeRef)) > 0 {
		for _, e := range edgesByType(edges, EdgeTypeRef) {
			if e.DstName == "Pt" {
				t.Errorf("typedef name must not be a type_ref: %+v", e)
			}
		}
	}
	// Calls inside a method are attributed to it.
	byID := map[string]*CodeSymbol{}
	for _, s := range syms {
		byID[s.ID] = s
	}
	found := false
	for _, e := range edgesByType(edges, EdgeCalls) {
		if e.DstName == "setX:withY:" {
			found = true
			if src := byID[e.SrcSymbolID]; src == nil || src.Name != "initWithFoo:bar:" {
				t.Errorf("setX:withY: not attributed to init: %+v", e)
			}
		}
	}
	if !found {
		t.Error("setX:withY: call missing")
	}
}

func TestDetectHeaderLanguage(t *testing.T) {
	cases := []struct {
		name, src string
		want      Language
	}{
		{"objc interface", "#import <Foundation/Foundation.h>\n@interface Foo : NSObject\n@end\n", LanguageObjectiveC},
		{"objc protocol", "@protocol P\n@end\n", LanguageObjectiveC},
		{"objc nsenum", "typedef NS_ENUM(NSInteger, M) { A };\n", LanguageObjectiveC},
		{"cpp class", "#pragma once\nnamespace a {\nclass B { public: int x; };\n}\n", LanguageCPP},
		{"cpp template", "template <typename T> T id(T t);\n", LanguageCPP},
		{"cpp std", "void f(std::string s);\n", LanguageCPP},
		{"c plain", "#ifndef X_H\n#define X_H\nstruct s { int a; };\nint f(int);\n#endif\n", LanguageC},
		{"c with objc in comment", "// @interface is ObjC\nint f(void);\n", LanguageC},
		{"c typedef struct", "typedef struct node { int v; } node;\n", LanguageC},
	}
	for _, c := range cases {
		if got := DetectHeaderLanguage([]byte(c.src)); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
	if l, ok := DetectLanguageForContent("x/y.h", []byte("@interface A\n@end")); !ok || l != LanguageObjectiveC {
		t.Errorf("DetectLanguageForContent .h = %s,%v", l, ok)
	}
	if l, _ := DetectLanguageForContent("y.m", []byte("int a;")); l != LanguageObjectiveC {
		t.Errorf(".m = %s", l)
	}
	if l, _ := DetectLanguage("y.h"); l != LanguageC {
		t.Errorf("DetectLanguage(.h) default = %s, want c", l)
	}
}
