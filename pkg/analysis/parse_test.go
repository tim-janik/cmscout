package analysis

import (
	"context"
	"errors"
	"testing"
)

func TestParse_headers(t *testing.T) {
	for _, test := range []struct{ source, language string }{
		{"typeof(int) value;\nint f(int);\n", "c"},
		{"class Widget { public: void render(); };\n", "cpp"},
		{"int f(int);\n", "cpp"},
	} {
		t.Run(test.language+test.source, func(t *testing.T) {
			ast, err := Parse(context.Background(), []byte(test.source), "api.h")
			if err != nil {
				t.Fatal(err)
			}
			defer ast.Close()
			if ast.Language().Name != test.language || ast.ErrorCount() != 0 {
				t.Fatalf("language=%s diagnostics=%+v", ast.Language().Name, ast.Diagnostics())
			}
			if ast.RootNode() == nil || string(ast.Source()) != test.source {
				t.Fatal("returned tree is not readable")
			}
		})
	}
}

func TestParse_invalid_inputs(t *testing.T) {
	if ast, err := Parse(context.Background(), nil, "file.unknown"); err == nil || ast != nil {
		t.Fatalf("unsupported language: ast=%v error=%v", ast, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, path := range []string{"file.go", "file.h"} {
		if ast, err := Parse(ctx, nil, path); !errors.Is(err, context.Canceled) || ast != nil {
			t.Fatalf("canceled parse: ast=%v error=%v", ast, err)
		}
	}
}

type cancel_before_fallback struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (ctx *cancel_before_fallback) Err() error {
	ctx.checks++
	if ctx.checks == 2 {
		ctx.cancel()
	}
	return ctx.Context.Err()
}

func TestParse_cancellation_before_header_fallback(t *testing.T) {
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &cancel_before_fallback{Context: base, cancel: cancel}
	ast, err := Parse(ctx, []byte("typeof(int) value;\n"), "api.h")
	if ast != nil {
		defer ast.Close()
	}
	if ctx.checks != 2 || ast != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("header selection ignored cancellation: checks=%d ast=%v error=%v", ctx.checks, ast, err)
	}
}

func TestParse_nil_context(t *testing.T) {
	for _, test := range []struct{ path, source, language string }{
		{"a.go", "package p\n", "go"},
		{"api.h", "typeof(int) value;\n", "c"},
	} {
		ast, err := Parse(nil, []byte(test.source), test.path)
		if err != nil {
			t.Fatal(err)
		}
		defer ast.Close()
		if ast.Language().Name != test.language || ast.RootNode() == nil {
			t.Fatalf("nil context changed parsing for %s", test.path)
		}
	}
}
