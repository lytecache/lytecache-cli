package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
)

// inferredValue parses raw per lytecache's default set-value type
// inference: an integer literal becomes int64, a float literal becomes
// float64, valid JSON (an object, array, bool, null, or quoted string)
// becomes its decoded form, and anything else is stored as the raw string.
//
// Integer and float literals are checked before generic JSON decoding
// specifically because encoding/json has no int type -- decoding a bare
// "42" as JSON would produce a float64, indistinguishable from a genuine
// float. Checking int/float first ensures "42" round-trips as an
// atomically-incrementable int (value_type 2), not a float or JSON number.
func inferredValue(raw string) any {
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		return f
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err == nil {
		return v
	}
	return raw
}

// parseTypedValue forces raw's interpretation per typeFlag ("int", "float",
// "json", or "string"), rather than inferring it. Callers handle
// typeFlag == "bytes" separately (see parseBytesValue), since that one
// needs --file/stdin, not just raw.
func parseTypedValue(raw, typeFlag string) (any, error) {
	switch typeFlag {
	case "int":
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid --type int value %q: %w", raw, err)
		}
		return n, nil
	case "float":
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid --type float value %q: %w", raw, err)
		}
		return f, nil
	case "json":
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, fmt.Errorf("invalid --type json value: %w", err)
		}
		return v, nil
	case "string":
		return raw, nil
	default:
		return nil, fmt.Errorf("unknown --type %q: must be one of string, int, float, json, bytes", typeFlag)
	}
}

// parseBytesValue implements --type bytes: raw bytes from --file, from
// stdin (when the value argument is exactly "-"), or otherwise raw
// interpreted as base64-encoded text.
func parseBytesValue(rawValue string, hasValue bool, fileFlag string, stdin io.Reader) ([]byte, error) {
	switch {
	case fileFlag != "":
		data, err := os.ReadFile(fileFlag)
		if err != nil {
			return nil, fmt.Errorf("reading --file %s: %w", fileFlag, err)
		}
		return data, nil
	case hasValue && rawValue == "-":
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}
		return data, nil
	case hasValue:
		data, err := base64.StdEncoding.DecodeString(rawValue)
		if err != nil {
			return nil, fmt.Errorf("invalid base64 value: %w", err)
		}
		return data, nil
	default:
		return nil, fmt.Errorf("--type bytes requires a base64-encoded value, --file <path>, or - to read stdin")
	}
}

// parseSetValue builds the Go value to pass to Cache.Set/Add/Replace from
// set's arguments -- the single place that turns CLI input into the value
// the core library will encode itself, so no encoding decision here
// duplicates what encodeValue already does.
func parseSetValue(rawValue string, hasValue bool, typeFlag, fileFlag string, stdin io.Reader) (any, error) {
	if typeFlag == "bytes" {
		return parseBytesValue(rawValue, hasValue, fileFlag, stdin)
	}
	if !hasValue {
		return nil, fmt.Errorf("a value is required unless --type bytes is used with --file or stdin")
	}
	if typeFlag == "" {
		return inferredValue(rawValue), nil
	}
	return parseTypedValue(rawValue, typeFlag)
}
