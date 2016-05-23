package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"sourcegraph.com/sourcegraph/srclib/unit"

	"github.com/jessevdk/go-flags"
)

var (
	config  *srcfileConfig = &srcfileConfig{}
	parser                 = flags.NewNamedParser("srclib-css", flags.Default)
	scanCmd ScanCmd        = ScanCmd{}
)

func init() {
	_, err := parser.AddCommand("scan",
		"scan for CSS files",
		"Scan the directory tree rooted at the current directory for CSS files.",
		&scanCmd,
	)
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	if _, err := parser.Parse(); err != nil {
		os.Exit(1)
	}
}

type srcfileConfig struct {
}

type ScanCmd struct{}

func (c *ScanCmd) Execute(args []string) error {
	if err := json.NewDecoder(os.Stdin).Decode(&config); err != nil {
		return err
	}
	if err := os.Stdin.Close(); err != nil {
		return err
	}

	CWD, err := os.Getwd()
	if err != nil {
		return err
	}

	// ScanCmd writes to Stdout only a single unit which represents all CSS files found on CWD.
	u := unit.SourceUnit{
		Name: filepath.Base(CWD),
		Type: "basic-css",
		Dir:  ".",
	}
	units := []*unit.SourceUnit{&u}

	// Walks the file tree rooted at CWD, for each file found: checks if it's file extension
	// is one of the following: "CSS", then adds it's file path to the unit's files.
	if err := filepath.Walk(CWD, func(path string, f os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if f.IsDir() {
			return nil
		}
		if isCSSFile(path) || isHTMLFile(path) {
			rp, err := filepath.Rel(CWD, path)
			if err != nil {
				return err
			}
			u.Files = append(u.Files, filepath.ToSlash(rp))
		}
		return nil
	}); err != nil {
		return err
	}

	b, err := json.MarshalIndent(units, "", "  ")
	if err != nil {
		return err
	}
	if _, err := os.Stdout.Write(b); err != nil {
		return err
	}
	return nil
}

func isCSSFile(filename string) bool {
	return filepath.Ext(filename) == ".css" && !strings.HasSuffix(filename, ".min.css")
}

func isHTMLFile(filename string) bool {
	f := strings.ToLower(filename)
	if filepath.Ext(f) == ".htm" && !strings.HasSuffix(f, ".min.htm") {
		return true
	}
	if filepath.Ext(f) == ".html" && !strings.HasSuffix(f, ".min.html") {
		return true
	}
	return false
}
