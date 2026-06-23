//go:build zvec && treesitter

package ast

import "testing"

func TestExtendScopePackage(t *testing.T) {
	base := PackageScope("main")
	got := extendScope(base, BoundaryMeta{Kind: "function", Name: "Auth"}, "go")
	want := "package main > func Auth"
	if got.String() != want {
		t.Fatalf("got %q want %q", got.String(), want)
	}
}

func TestBoundaryKindFromCapture(t *testing.T) {
	if boundaryKindFromCapture("boundary.method") != "method" {
		t.Fatal()
	}
	if boundaryKindFromCapture("boundary.assignment") != "module_var" {
		t.Fatal()
	}
	if boundaryKindFromCapture("boundary.interface") != "interface" {
		t.Fatal()
	}
}

func TestClassScopeOnly(t *testing.T) {
	if classScopeOnly(ModuleScope("m")) != "module m" {
		t.Fatal()
	}
}

func TestExtendScopeClass(t *testing.T) {
	got := extendScope(ModuleScope("s"), BoundaryMeta{Kind: "class", Name: "C"}, "python").String()
	if got != "module s > class C" {
		t.Fatalf("got %q", got)
	}
}
