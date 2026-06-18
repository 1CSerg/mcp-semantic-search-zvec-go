package ast

import "testing"

func TestExtendScopePackage(t *testing.T) {
	base := PackageScope("main")
	got := extendScope(base, BoundaryMeta{Kind: "function", Name: "Auth"})
	want := "package main > func Auth"
	if got.String() != want {
		t.Fatalf("got %q want %q", got.String(), want)
	}
}

func TestBoundaryKindFromCapture(t *testing.T) {
	if boundaryKindFromCapture("boundary.method") != "method" {
		t.Fatal()
	}
}
