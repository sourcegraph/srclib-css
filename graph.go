package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"os"

	cssParser "github.com/aymerick/douceur/parser"
	"sourcegraph.com/sourcegraph/srclib/graph"
	"sourcegraph.com/sourcegraph/srclib/unit"
)

var (
	cssRefURL string = "https://developer.mozilla.org/en-US/docs/Web/CSS/"
)

func init() {
	_, err := parser.AddCommand("graph",
		"graph a CSS file",
		"Graph a CSS file, producing all defs, refs, and docs.",
		&graphCmd,
	)
	if err != nil {
		log.Fatal(err)
	}
}

type GraphCmd struct{}

var graphCmd GraphCmd

func (c *GraphCmd) Execute(args []string) error {
	inputBytes, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	var units unit.SourceUnits
	if err := json.NewDecoder(bytes.NewReader(inputBytes)).Decode(&units); err != nil {
		// Legacy API: try parsing input as a single source unit
		var u *unit.SourceUnit
		if err := json.NewDecoder(bytes.NewReader(inputBytes)).Decode(&u); err != nil {
			return err
		}
		units = unit.SourceUnits{u}
	}
	if err := os.Stdin.Close(); err != nil {
		return err
	}
	if len(units) == 0 {
		log.Fatal("Input contains no source unit data.")
	}
	out, err := Graph(units)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		return err
	}
	return nil
}

func Graph(units unit.SourceUnits) (*graph.Output, error) {
	f, err := ioutil.ReadFile("./testdata/main.css")
	if err != nil {
		return nil, err
	}
	stylesheet, err := cssParser.Parse(string(f))
	if err != nil {
		return nil, err
	}
	out := graph.Output{Refs: []*graph.Ref{}}
	for _, r := range stylesheet.Rules {
		declarations := r.Declarations
		for _, d := range declarations {
			out.Refs = append(out.Refs, &graph.Ref{DefUnitType: "URL", DefPath: defPath(d.Property)})
		}
	}
	return &out, nil
}

func defPath(cssProperty string) string {
	return cssRefURL + cssProperty
}
