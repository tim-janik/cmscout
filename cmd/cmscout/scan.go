// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package main

import (
	"flag"
	"os"

	"cmscout/pkg/source"
)

type scan_flags struct {
	scan    bool
	root    string
	include []string
	exclude []string
}

func (sf *scan_flags) register(fs *flag.FlagSet) {
	fs.BoolVar(&sf.scan, "scan", false, "scan files and directories for stats or metrics")
	fs.StringVar(&sf.root, "root", "", "source root for names and scan filters")
	fs.Func("include", "include root-relative scan glob, repeatable", func(value string) error {
		sf.include = append(sf.include, value)
		return nil
	})
	fs.Func("exclude", "exclude root-relative scan glob, repeatable", func(value string) error {
		sf.exclude = append(sf.exclude, value)
		return nil
	})
}

func (sf scan_flags) selected(paths []string, redirected bool) bool {
	if sf.scan {
		return true
	}
	if len(paths) == 1 && !redirected {
		info, err := os.Stat(paths[0])
		return err == nil && info.IsDir()
	}
	return false
}

func (sf scan_flags) files(paths []string) (*source.FileSet, error) {
	return source.Scan(paths, sf.root, source.Filter{Include: sf.include, Exclude: sf.exclude})
}
