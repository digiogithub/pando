package treesitter

import "testing"

const rubySrc = `require 'json'
require_relative "lib/util"
module Outer
  class Foo < Base
    include Comparable
    extend Forwardable
    MAX = 3
    attr_accessor :name, :age
    def initialize(a); @a = a; end
    def self.build; new; end
    class << self
      def helper; end
    end
    def run
      puts "x"
      @a.to_s
      Util.go(1)
    end
  end
end
class A::B < ::C; end
def top; end
`

func TestRubyExtractor(t *testing.T) {
	walker := NewASTWalker(DefaultWalkerConfig())
	parser := NewParser()
	defer parser.Close()
	syms, edges := parseAndExtract(t, walker, parser, LanguageRuby, "foo.rb", rubySrc)

	want := []struct {
		name string
		st   SymbolType
	}{
		{"Outer", SymbolTypeModule},
		{"Foo", SymbolTypeClass},
		{"A::B", SymbolTypeClass},
		{"initialize", SymbolTypeConstructor},
		{"self.build", SymbolTypeMethod},
		{"self.helper", SymbolTypeMethod},
		{"run", SymbolTypeMethod},
		{"top", SymbolTypeFunction},
		{"MAX", SymbolTypeConstant},
		{"name", SymbolTypeProperty},
		{"age", SymbolTypeProperty},
	}
	for _, w := range want {
		if symByName(syms, w.name, w.st) == nil {
			t.Errorf("missing %s %q", w.st, w.name)
		}
	}
	if s := symByName(syms, "run", SymbolTypeMethod); s != nil && s.NamePath != "/Outer/Foo/run" {
		t.Errorf("run name path = %q", s.NamePath)
	}

	for _, p := range []string{"json", "lib/util"} {
		if !hasImport(edges, p) {
			t.Errorf("missing import %q: %+v", p, edgesByType(edges, EdgeImports))
		}
	}
	for _, c := range []string{"puts", "to_s", "go"} {
		if !hasEdge(edges, EdgeCalls, c) {
			t.Errorf("missing call %q: %+v", c, edgesByType(edges, EdgeCalls))
		}
	}
	for _, c := range []string{"require", "include", "attr_accessor"} {
		if hasEdge(edges, EdgeCalls, c) {
			t.Errorf("%q must not be a call edge", c)
		}
	}
	for _, r := range []string{"Base", "Comparable", "Forwardable", "Util", "C"} {
		if !hasEdge(edges, EdgeTypeRef, r) {
			t.Errorf("missing type_ref %q: %+v", r, edgesByType(edges, EdgeTypeRef))
		}
	}
	run := symByName(syms, "run", SymbolTypeMethod)
	for _, e := range edgesByType(edges, EdgeCalls) {
		if e.DstName == "go" && e.SrcSymbolID != run.ID {
			t.Errorf("go call not attributed to run: %+v", e)
		}
	}
}
