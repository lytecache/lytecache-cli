package ui

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"

	lytecache "github.com/lytecache/lytecache-go"
)

// Value type codes, matching SPEC.md. Duplicated from
// cmd/lytecache/output.go rather than imported: the core library keeps
// these unexported (they're wire-format constants, not behavior), and this
// package cannot import cmd/lytecache anyway, since that's package main.
const (
	typeBytes  = 0
	typeString = 1
	typeInt    = 2
	typeFloat  = 3
	typeJSON   = 4
	typePython = 5
	typeJava   = 6
)

// typeCodeName returns the SPEC.md name for a value_type code.
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

// nonPortableMessage is shown instead of a decoded value for codes 5/6.
func nonPortableMessage(code int, sizeBytes int64) string {
	return fmt.Sprintf("written by another language's native serializer (%s, %d bytes) -- not viewable here", typeCodeName(code), sizeBytes)
}

// getDecodedValue reads key's value as the Go type matching its stored
// type code, given a code already known from Inspect. See output.go's
// getDecoded (cmd/lytecache) for why this is keyed off the known code
// rather than decoding into *any and switching on the resulting Go type.
func getDecodedValue(c *lytecache.Cache, key string, typeCode int) (any, bool, error) {
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

// renderValue renders an already-decoded value for the value viewer.
// Bytes render as base64 plus a byte count (never raw binary into HTML);
// JSON is pretty-printed with indentation for the collapsible tree view.
func renderValue(typeCode int, value any) (string, error) {
	switch typeCode {
	case typeBytes:
		b, ok := value.([]byte)
		if !ok {
			return "", fmt.Errorf("expected []byte for a bytes value, got %T", value)
		}
		return fmt.Sprintf("%s\n(%d bytes, base64)", base64.StdEncoding.EncodeToString(b), len(b)), nil
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
		b, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return "", err
		}
		return string(b), nil
	default:
		return "", fmt.Errorf("unsupported value_type=%d", typeCode)
	}
}
