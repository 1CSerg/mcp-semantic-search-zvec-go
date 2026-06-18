package main

import (
	"fmt"
	"os"
)

// Small imports grouped into one chunk.
import (
	"strings"
	"time"
)

// MyFunc fits in a single AST chunk.
func MyFunc() {
	fmt.Println("hello")
	for i := 0; i < 3; i++ {
		fmt.Println(i)
	}
}

// OversizedFunc exceeds typical test budgets and triggers partial fallback.
func OversizedFunc() {
	line := "x"
	line += "y"
	if line == "xy" {
		fmt.Println("big block start")
		fmt.Println("line 1")
		fmt.Println("line 2")
		fmt.Println("line 3")
		fmt.Println("line 4")
		fmt.Println("line 5")
		fmt.Println("line 6")
		fmt.Println("line 7")
		fmt.Println("line 8")
		fmt.Println("line 9")
		fmt.Println("line 10")
		fmt.Println("line 11")
		fmt.Println("line 12")
		fmt.Println("line 13")
		fmt.Println("line 14")
		fmt.Println("line 15")
		fmt.Println("line 16")
		fmt.Println("line 17")
		fmt.Println("line 18")
		fmt.Println("line 19")
		fmt.Println("line 20")
		fmt.Println("big block end")
	}
	_ = os.Stdout
	_ = time.Now
	_ = strings.Trim
}

type Server struct{}

func (s *Server) Foo() {}
func (s Server) Bar()  {}

const (
	A = 1
	B = 2
)

var (
	x = 1
	y = 2
)

type (
	T1 struct{}
	T2 struct{ N int }
)
