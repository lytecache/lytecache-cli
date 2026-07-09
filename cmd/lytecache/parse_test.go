package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestInferredValue(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want any
	}{
		{"integer", "42", int64(42)},
		{"negative integer", "-7", int64(-7)},
		{"float", "3.14", float64(3.14)},
		{"json object", `{"a":1}`, map[string]any{"a": float64(1)}},
		{"json array", "[1,2,3]", []any{float64(1), float64(2), float64(3)}},
		{"json bool", "true", true},
		{"json null", "null", nil},
		{"quoted json string", `"hello"`, "hello"},
		{"plain string", "hello world", "hello world"},
		{"empty string", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := inferredValue(tc.raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("inferredValue(%q) = %#v (%T), want %#v (%T)", tc.raw, got, got, tc.want, tc.want)
			}
		})
	}
}

func TestParseTypedValue(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		typeFlag string
		want     any
		wantErr  bool
	}{
		{"forced int", "42", "int", int64(42), false},
		{"forced int on non-integer", "42.5", "int", nil, true},
		{"forced float", "3", "float", float64(3), false},
		{"forced json", `{"a":1}`, "json", map[string]any{"a": float64(1)}, false},
		{"forced json invalid", "not json", "json", nil, true},
		{"forced string", "42", "string", "42", false},
		{"unknown type flag", "x", "bogus", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTypedValue(tc.raw, tc.typeFlag)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseTypedValue(%q, %q) = %#v, want an error", tc.raw, tc.typeFlag, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTypedValue(%q, %q) unexpected error: %v", tc.raw, tc.typeFlag, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseTypedValue(%q, %q) = %#v, want %#v", tc.raw, tc.typeFlag, got, tc.want)
			}
		})
	}
}

func TestParseBytesValue(t *testing.T) {
	t.Run("base64", func(t *testing.T) {
		got, err := parseBytesValue("aGk=", true, "", strings.NewReader(""))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(got) != "hi" {
			t.Errorf("got %q, want %q", got, "hi")
		}
	})

	t.Run("invalid base64", func(t *testing.T) {
		if _, err := parseBytesValue("not-base64!!", true, "", strings.NewReader("")); err == nil {
			t.Fatal("want an error for invalid base64")
		}
	})

	t.Run("stdin", func(t *testing.T) {
		got, err := parseBytesValue("-", true, "", strings.NewReader("raw stdin bytes"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(got) != "raw stdin bytes" {
			t.Errorf("got %q, want %q", got, "raw stdin bytes")
		}
	})

	t.Run("no value and no file", func(t *testing.T) {
		if _, err := parseBytesValue("", false, "", strings.NewReader("")); err == nil {
			t.Fatal("want an error when neither a value nor --file is given")
		}
	})
}

func TestParseSetValue(t *testing.T) {
	t.Run("no value without --type bytes is an error", func(t *testing.T) {
		if _, err := parseSetValue("", false, "", "", strings.NewReader("")); err == nil {
			t.Fatal("want an error")
		}
	})

	t.Run("inference used when no --type given", func(t *testing.T) {
		got, err := parseSetValue("42", true, "", "", strings.NewReader(""))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != int64(42) {
			t.Errorf("got %#v, want int64(42)", got)
		}
	})

	t.Run("--type bytes routes to parseBytesValue even without a value", func(t *testing.T) {
		got, err := parseSetValue("", false, "bytes", "", strings.NewReader(""))
		if err == nil {
			t.Fatalf("want an error (no value, no file, no stdin marker), got %#v", got)
		}
	})
}
