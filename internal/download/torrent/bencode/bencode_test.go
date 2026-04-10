package bencode

import (
	"testing"
)

func TestDecodeInt(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		hasError bool
	}{
		{"i0e", 0, false},
		{"i1e", 1, false},
		{"i-1e", -1, false},
		{"i123456789e", 123456789, false},
		{"i-123456789e", -123456789, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			val, err := Decode([]byte(tt.input))
			if tt.hasError {
				if err == nil {
					t.Errorf("expected error for %s", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			i, ok := val.(Int)
			if !ok {
				t.Errorf("expected Int, got %T", val)
				return
			}
			if int64(i) != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, int64(i))
			}
		})
	}
}

func TestDecodeString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		hasError bool
	}{
		{"4:spam", "spam", false},
		{"0:", "", false},
		{"", "", true},
		{"abc", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			val, err := Decode([]byte(tt.input))
			if tt.hasError {
				if err == nil {
					t.Errorf("expected error for %s", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			s, ok := val.(String)
			if !ok {
				t.Errorf("expected String, got %T", val)
				return
			}
			if string(s) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, string(s))
			}
		})
	}
}

func TestDecodeList(t *testing.T) {
	tests := []struct {
		input    string
		expected []interface{}
		hasError bool
	}{
		{"le", []interface{}{}, false},
		{"li1ei2ei3ee", []interface{}{Int(1), Int(2), Int(3)}, false},
		{"l4:spam4:eggse", []interface{}{String("spam"), String("eggs")}, false},
		{"l4:spami123ee", []interface{}{String("spam"), Int(123)}, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			val, err := Decode([]byte(tt.input))
			if tt.hasError {
				if err == nil {
					t.Errorf("expected error for %s", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			list, ok := val.(List)
			if !ok {
				t.Errorf("expected List, got %T", val)
				return
			}
			if len(list) != len(tt.expected) {
				t.Errorf("expected %d elements, got %d", len(tt.expected), len(list))
			}
		})
	}
}

func TestDecodeDict(t *testing.T) {
	input := "d3:foo3:bar4:spami42ee"
	val, err := Decode([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	d, ok := val.(Dict)
	if !ok {
		t.Fatalf("expected Dict, got %T", val)
	}

	if d["foo"] != String("bar") {
		t.Errorf("expected foo=bar, got foo=%v", d["foo"])
	}
	if d["spam"] != Int(42) {
		t.Errorf("expected spam=42, got spam=%v", d["spam"])
	}
}

func TestEncodeInt(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "i0e"},
		{1, "i1e"},
		{-1, "i-1e"},
		{123456789, "i123456789e"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result, err := Encode(Int(tt.input))
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if string(result) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, string(result))
			}
		})
	}
}

func TestEncodeString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"spam", "4:spam"},
		{"", "0:"},
		{"hello world", "11:hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result, err := Encode(tt.input)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if string(result) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, string(result))
			}
		})
	}
}

func TestEncodeList(t *testing.T) {
	input := List{Int(1), Int(2), Int(3)}
	result, err := Encode(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != "li1ei2ei3ee" {
		t.Errorf("expected li1ei2ei3ee, got %s", string(result))
	}
}

func TestEncodeDict(t *testing.T) {
	input := Dict{
		"foo":  String("bar"),
		"spam": Int(42),
	}
	result, err := Encode(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) == 0 {
		t.Error("expected non-empty result")
	}
}

func TestDecodeInfoDict(t *testing.T) {
	input := "d4:infod6:lengthi12345eeee"
	val, err := DecodeInfoDict([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if val["length"] != Int(12345) {
		t.Errorf("expected length=12345, got %v", val["length"])
	}
}

func TestRoundTrip(t *testing.T) {
	original := Dict{
		"name":  String("test"),
		"count": Int(100),
	}

	encoded, err := Encode(original)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	d := decoded.(Dict)
	if d["name"] != original["name"] {
		t.Error("round trip failed: name mismatch")
	}
}
