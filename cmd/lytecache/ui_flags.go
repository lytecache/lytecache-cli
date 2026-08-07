package main

import (
	"fmt"
	"strings"

	"github.com/lytecache/lytecache-cli/cmd/lytecache/ui"
)

// dbSourceFlagValue implements pflag.Value so --db name=/path/to.db can be
// repeated on the command line, appending to the target slice each time
// (pflag calls Set once per occurrence of the flag).
type dbSourceFlagValue struct {
	dbs *[]ui.DBSource
}

func newDBSourceFlagValue(dbs *[]ui.DBSource) *dbSourceFlagValue {
	return &dbSourceFlagValue{dbs: dbs}
}

func (v *dbSourceFlagValue) String() string { return "" }
func (v *dbSourceFlagValue) Type() string   { return "name=path" }

func (v *dbSourceFlagValue) Set(s string) error {
	name, path, ok := strings.Cut(s, "=")
	if !ok || name == "" || path == "" {
		return fmt.Errorf("expected name=path, got %q", s)
	}
	expanded, err := expandHome(path)
	if err != nil {
		return err
	}
	*v.dbs = append(*v.dbs, ui.DBSource{Name: name, Path: expanded})
	return nil
}
