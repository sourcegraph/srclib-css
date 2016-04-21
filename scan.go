package main

import (
	"encoding/json"
	"log"
	"os"

	"sourcegraph.com/sourcegraph/srclib/unit"

	"github.com/jessevdk/go-flags"
)

var (
	parser = flags.NewNamedParser("srclib-css", flags.Default)
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

type ScanCmd struct{}

var scanCmd ScanCmd

func (c *ScanCmd) Execute(args []string) error {
	// TODO: handle the Stadin: JSON object representation of repository config (typically {}).

	// TODO: Actually scan all the CSS files units present on the current directory?.
	u := unit.SourceUnit{
		Name: "testdata/main.css",
		Type: "CSSFile",
	}
	units := []*unit.SourceUnit{&u}
	b, err := json.MarshalIndent(units, "", "  ")
	if err != nil {
		return err
	}
	if _, err := os.Stdout.Write(b); err != nil {
		return err
	}
	return nil
}

func main() {
	if _, err := parser.Parse(); err != nil {
		os.Exit(1)
	}
}
