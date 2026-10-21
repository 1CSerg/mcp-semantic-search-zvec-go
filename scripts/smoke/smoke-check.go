//go:build ignore

// smoke-check validates Phase 1 smoke HTTP responses without external tooling.
//
// Usage:
//
//	smoke-check free-port                  # prints an unused loopback TCP port
//	smoke-check status            < /v1/status.json
//	smoke-check results <min> <glob|-> < /v1/search.json
//
// Exit code 1 means the response does not satisfy the smoke contract; the reason
// is printed to stderr so the caller can surface it together with the server log.
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: smoke-check free-port|status|results <min> <glob|->")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "free-port":
		err = printFreePort()
	case "status":
		err = checkStatus()
	case "results":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "usage: smoke-check results <min> <glob|->")
			os.Exit(2)
		}
		err = checkResults(os.Args[2], os.Args[3])
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q\n", os.Args[1])
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
}

func printFreePort() error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return err
	}
	fmt.Println(port)
	return nil
}

func decode(dst any) error {
	dec := json.NewDecoder(os.Stdin)
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	return nil
}

func checkStatus() error {
	var status struct {
		ZvecOpenOK   *bool   `json:"zvec_open_ok"`
		ZvecDocCount *int64  `json:"zvec_doc_count"`
		ZvecError    *string `json:"zvec_error"`
		Message      *string `json:"message"`
		Collection   string  `json:"zvec_collection"`
	}
	if err := decode(&status); err != nil {
		return err
	}
	if status.ZvecOpenOK == nil || !*status.ZvecOpenOK {
		return fmt.Errorf("zvec_open_ok=%v zvec_doc_count=%v zvec_error=%v message=%v",
			derefBool(status.ZvecOpenOK), derefInt(status.ZvecDocCount), derefStr(status.ZvecError), derefStr(status.Message))
	}
	if status.ZvecDocCount == nil || *status.ZvecDocCount < 1 {
		return fmt.Errorf("seeded index not visible: zvec_doc_count=%v zvec_error=%v message=%v",
			derefInt(status.ZvecDocCount), derefStr(status.ZvecError), derefStr(status.Message))
	}
	fmt.Printf("  status: zvec_open_ok=true zvec_doc_count=%d collection=%s\n", *status.ZvecDocCount, status.Collection)
	return nil
}

type searchResult struct {
	Path    string   `json:"path"`
	Score   *float64 `json:"score"`
	Snippet string   `json:"snippet"`
}

func checkResults(minArg, glob string) error {
	min, err := strconv.Atoi(minArg)
	if err != nil {
		return fmt.Errorf("min results %q: %w", minArg, err)
	}
	var resp struct {
		Results []searchResult `json:"results"`
		Message string         `json:"message"`
	}
	if err := decode(&resp); err != nil {
		return err
	}
	if len(resp.Results) < min {
		return fmt.Errorf("%d results, want >= %d (message=%q)", len(resp.Results), min, resp.Message)
	}
	label := "plain search"
	if glob != "-" {
		label = "path_glob " + glob
	}
	for _, r := range resp.Results {
		if r.Path == "" || r.Score == nil {
			return fmt.Errorf("%s: result missing path/score: %+v", label, r)
		}
		if glob == "-" {
			continue
		}
		ok, matchErr := matchGlob(r.Path, glob)
		if matchErr != nil {
			return fmt.Errorf("%s: glob %q: %w", label, glob, matchErr)
		}
		if !ok {
			return fmt.Errorf("%s: non-matching path %q", label, r.Path)
		}
	}
	if glob == "-" {
		fmt.Printf("PASS Phase 1 smoke: %d results, top score=%v\n", len(resp.Results), *resp.Results[0].Score)
		fmt.Printf("  path=%s snippet=%s\n", resp.Results[0].Path, truncate(resp.Results[0].Snippet, 80))
	}
	return nil
}

// matchGlob implements the path_glob semantics used by the server: a glob without
// a slash matches the basename at any depth, and ** crosses directories.
func matchGlob(p, glob string) (bool, error) {
	p = strings.ReplaceAll(p, "\\", "/")
	if !strings.Contains(glob, "/") {
		return path.Match(glob, path.Base(p))
	}
	if strings.HasPrefix(glob, "**/") {
		if ok, err := path.Match(glob[3:], p); err != nil || ok {
			return ok, err
		}
		if ok, err := path.Match(glob[3:], path.Base(p)); err != nil || ok {
			return ok, err
		}
	}
	return path.Match(glob, p)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func derefBool(v *bool) any {
	if v == nil {
		return nil
	}
	return *v
}

func derefInt(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func derefStr(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}
