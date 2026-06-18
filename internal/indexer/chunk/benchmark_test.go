//go:build zvec && treesitter

// Hybrid vs line_window benchmark gate (BENCH_CHECK=1). Reduced fixtures by default;
// set BENCH_FULL=1 for the 1000/200/200 set. Generate fixtures offline with:
//
//	python scripts/dev/generate-chunk-benchmark-fixtures.py /tmp/chunk-bench --go 1000 --tsx 200 --bsl 200
package chunk

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
)

func generateBenchmarkFixtures(root string, goN, tsxN, bslN int) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	for i := 0; i < goN; i++ {
		broken := i%10 == 0
		path := filepath.Join(root, fmt.Sprintf("file_%04d.go", i))
		if broken {
			if err := os.WriteFile(path, []byte("package broken\n\nfunc {{{() {\n"), 0o644); err != nil {
				return err
			}
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "package pkg%d\n\n", i%50)
		for fn := 0; fn < 5+i%10; fn++ {
			fmt.Fprintf(&b, "func Func%d_%d() int {\n\treturn %d\n}\n\n", i, fn, fn)
		}
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			return err
		}
	}
	for i := 0; i < tsxN; i++ {
		broken := i%10 == 0
		path := filepath.Join(root, fmt.Sprintf("comp_%04d.tsx", i))
		if broken {
			if err := os.WriteFile(path, []byte("export function Broken() { return <div></ ; }\n"), 0o644); err != nil {
				return err
			}
			continue
		}
		content := fmt.Sprintf(`import React from 'react';

export interface Props%d {
  label: string;
}

export function Component%d(props: Props%d) {
  return <button>{props.label}</button>;
}
`, i, i, i)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	for i := 0; i < bslN; i++ {
		broken := i%10 == 0
		path := filepath.Join(root, fmt.Sprintf("mod_%04d.bsl", i))
		if broken {
			if err := os.WriteFile(path, []byte("Процедура Broken(\nКонецПроцедуры\n"), 0o644); err != nil {
				return err
			}
			continue
		}
		content := fmt.Sprintf(`#Область Area%d

Процедура Proc%d() Экспорт
	Сообщить("%d");
КонецПроцедуры

Функция Fn%d() Экспорт
	Возврат %d;
КонецФункции

#КонецОбласти
`, i, i, i, i, i)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func hybridBenchOpts() Options {
	return Options{
		ChunkingStrategy: "hybrid",
		MaxInputTokens:   512,
		EmbedBudgetRatio: 1.0,
		MinChunkTokens:   1,
		WindowLines:      40,
		OverlapLines:     8,
		Languages: map[string]config.LanguageConfig{
			"go":         {Enabled: true},
			"typescript": {Enabled: true},
			"bsl":        {Enabled: true, IncludeSDBL: true},
		},
	}
}

func lineWindowBenchOpts() Options {
	opts := hybridBenchOpts()
	opts.ChunkingStrategy = "line_window"
	return opts
}

type benchResult struct {
	elapsed   time.Duration
	heapInuse uint64
	chunks    int
}

func runChunkBenchmark(root string, opts Options) (benchResult, error) {
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	counter := token.CharCounter{}
	total := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".go" && ext != ".tsx" && ext != ".bsl" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		n, err := ProcessBatches(root, filepath.ToSlash(rel), opts, counter, 32, func(batch []zvec.Chunk) error {
			return nil
		})
		if err != nil {
			return err
		}
		total += n
		return nil
	})
	elapsed := time.Since(start)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	var delta uint64
	if after.HeapInuse >= before.HeapInuse {
		delta = after.HeapInuse - before.HeapInuse
	}
	return benchResult{elapsed: elapsed, heapInuse: delta, chunks: total}, err
}

func benchmarkFixtureCounts() (goN, tsxN, bslN int) {
	if os.Getenv("BENCH_FULL") == "1" {
		return 1000, 200, 200
	}
	return 50, 10, 10
}

func TestBenchmarkHybridWithin2x(t *testing.T) {
	if testing.Short() {
		t.Skip("benchmark ratio check skipped in -short mode")
	}
	if os.Getenv("BENCH_CHECK") != "1" {
		t.Skip("set BENCH_CHECK=1 to run hybrid vs line_window ratio gate (use BENCH_FULL=1 for full 1000/200/200 set)")
	}
	root := t.TempDir()
	goN, tsxN, bslN := benchmarkFixtureCounts()
	if err := generateBenchmarkFixtures(root, goN, tsxN, bslN); err != nil {
		t.Fatal(err)
	}
	lw, err := runChunkBenchmark(root, lineWindowBenchOpts())
	if err != nil {
		t.Fatal(err)
	}
	hy, err := runChunkBenchmark(root, hybridBenchOpts())
	if err != nil {
		t.Fatal(err)
	}
	if lw.chunks == 0 || hy.chunks == 0 {
		t.Fatalf("no chunks: line_window=%d hybrid=%d", lw.chunks, hy.chunks)
	}
	timeRatio := float64(hy.elapsed) / float64(lw.elapsed)
	if timeRatio > 2.0 {
		t.Fatalf("hybrid wall time %.2fx line_window (limit 2x): lw=%v hy=%v", timeRatio, lw.elapsed, hy.elapsed)
	}
	if lw.heapInuse > 0 && hy.heapInuse > lw.heapInuse*2 {
		memRatio := float64(hy.heapInuse) / float64(lw.heapInuse)
		t.Fatalf("hybrid heap_inuse %.2fx line_window (limit 2x): lw=%d hy=%d", memRatio, lw.heapInuse, hy.heapInuse)
	}
	t.Logf("bench go=%d tsx=%d bsl=%d lw=%v hy=%v time_ratio=%.2f heap_lw=%d heap_hy=%d", goN, tsxN, bslN, lw.elapsed, hy.elapsed, timeRatio, lw.heapInuse, hy.heapInuse)
}

func BenchmarkHybridVsLineWindow(b *testing.B) {
	root := b.TempDir()
	goN, tsxN, bslN := benchmarkFixtureCounts()
	if os.Getenv("BENCH_FULL") == "1" {
		goN, tsxN, bslN = 1000, 200, 200
	}
	if err := generateBenchmarkFixtures(root, goN, tsxN, bslN); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.Run("line_window", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := runChunkBenchmark(root, lineWindowBenchOpts()); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("hybrid", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := runChunkBenchmark(root, hybridBenchOpts()); err != nil {
				b.Fatal(err)
			}
		}
	})
}
