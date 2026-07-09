package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"

	lytecache "github.com/lytecache/lytecache-go"
)

// Value type codes, matching SPEC.md. Named here (not imported) because the
// core library keeps these unexported -- they are wire-format constants,
// not behavior, so mirroring them is not "cache logic".
const (
	typeBytes  = 0
	typeString = 1
	typeInt    = 2
	typeFloat  = 3
	typeJSON   = 4
	typePython = 5
	typeJava   = 6
)

// typeCodeName returns the SPEC.md name for a value_type code, used in
// `dump`, `keys --long`, and the non-portable-value message.
func typeCodeName(code int) string {
	switch code {
	case typeBytes:
		return "bytes"
	case typeString:
		return "string"
	case typeInt:
		return "int"
	case typeFloat:
		return "float"
	case typeJSON:
		return "json"
	case typePython:
		return "python-pickle"
	case typeJava:
		return "java-serialized"
	default:
		return fmt.Sprintf("unknown(%d)", code)
	}
}

// isNonPortable reports whether code is a language-specific escape hatch
// (Python pickle or Java serialization) that Go cannot decode.
func isNonPortable(code int) bool {
	return code == typePython || code == typeJava
}

// nonPortableMessage is the message shown instead of a decoded value for
// codes 5/6, everywhere that would otherwise print one (get, dump, keys
// --long).
func nonPortableMessage(code int, sizeBytes int64) string {
	return fmt.Sprintf("(non-portable value: %s, %d bytes)", typeCodeName(code), sizeBytes)
}

// formatDecodedValue renders an already-decoded value (from Get, typically
// via a *any destination for JSON, or a typed getter for the other codes)
// for display. raw selects exact/compact form over pretty-printing --
// currently only meaningful for JSON (default: indented; raw: compact,
// matching the bytes actually written to storage).
func formatDecodedValue(typeCode int, value any, raw bool) (string, error) {
	switch typeCode {
	case typeBytes:
		b, ok := value.([]byte)
		if !ok {
			return "", fmt.Errorf("expected []byte for a bytes value, got %T", value)
		}
		if raw {
			return string(b), nil
		}
		return fmt.Sprintf("%s\n(bytes, %d B)", base64.StdEncoding.EncodeToString(b), len(b)), nil
	case typeString:
		s, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("expected string for a string value, got %T", value)
		}
		return s, nil
	case typeInt:
		n, ok := value.(int64)
		if !ok {
			return "", fmt.Errorf("expected int64 for an int value, got %T", value)
		}
		return strconv.FormatInt(n, 10), nil
	case typeFloat:
		f, ok := value.(float64)
		if !ok {
			return "", fmt.Errorf("expected float64 for a float value, got %T", value)
		}
		return strconv.FormatFloat(f, 'g', -1, 64), nil
	case typeJSON:
		if raw {
			b, err := json.Marshal(value)
			if err != nil {
				return "", err
			}
			return string(b), nil
		}
		b, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return "", err
		}
		return string(b), nil
	default:
		return "", fmt.Errorf("unsupported value_type=%d", typeCode)
	}
}

// getDecoded reads key's value as the Go type matching its stored type
// code (int64/float64/string/[]byte, or the JSON-decoded value for JSON),
// given a type code already known from Inspect. Keeping this keyed off the
// known code -- rather than decoding into *any and switching on the
// resulting Go type -- matters because a JSON value that happens to be a
// bare number would otherwise be indistinguishable from a genuine FLOAT
// value: both decode to a Go float64.
func getDecoded(c *lytecache.Cache, key string, typeCode int) (any, bool, error) {
	switch typeCode {
	case typeBytes:
		return c.GetBytes(key)
	case typeString:
		return c.GetString(key)
	case typeInt:
		return c.GetInt64(key)
	case typeFloat:
		return c.GetFloat64(key)
	case typeJSON:
		var v any
		found, err := c.Get(key, &v)
		return v, found, err
	default:
		return nil, false, fmt.Errorf("unsupported value_type=%d", typeCode)
	}
}
