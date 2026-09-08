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
