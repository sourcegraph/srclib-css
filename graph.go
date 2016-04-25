package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"os"

	cssParser "github.com/aymerick/douceur/parser"
	"sourcegraph.com/sourcegraph/srclib/graph"
	"sourcegraph.com/sourcegraph/srclib/unit"
)

var (
	// mdnCSSReferenceURL is mozilla developer network CSS reference root URL.
	mdnCSSReferenceURL string = "https://developer.mozilla.org/en-US/docs/Web/CSS/"
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
	// Expecting one unit, further info see: ScanCmd.Execute method.
	if len(units) > 1 {
		return nil, errors.New("unexpected multiple units")
	}
	u := units[0]

	out := graph.Output{Refs: []*graph.Ref{}}

	// Iterate over u.Files, for each file the process performed can be described as follow:
	// 1. The file is CSS parsed.
	// 2. For each CSS property found on the parsed file a graph.Ref is created & appended to out.Refs.
	for _, currentFile := range u.Files {
		f, err := ioutil.ReadFile(currentFile)
		if err != nil {
			return nil, err
		}
		file := string(f)
		stylesheet, err := cssParser.Parse(file)
		if err != nil {
			return nil, err
		}
		for _, r := range stylesheet.Rules {
			declarations := r.Declarations
			for _, d := range declarations {
				s, e := findOffsets(file, d.Line, d.Column, d.Property)
				out.Refs = append(out.Refs, &graph.Ref{
					DefUnitType: "URL",
					DefUnit:     "MDN",
					DefPath:     mdnDefPath(d.Property),
					Unit:        u.Name,
					File:        currentFile,
					Start:       uint32(s),
					End:         uint32(e),
				})
			}
		}
	}
	return &out, nil
}

// findOffsets discovers the start & end offset of given token on fileText, uses the given line & column as input
// to discover the start offset which is used to calculate the end offset.
// Returns (-1, -1) if offsets were not found.
func findOffsets(fileText string, line, column int, token string) (start, end int) {

	// we count our current line and column position.
	currentCol := 1
	currentLine := 1

	for offset, ch := range fileText {
		// see if we found where we wanted to go to.
		if currentLine == line && currentCol == column {
			end = offset + len([]byte(token))
			return offset, end
		}

		// line break - increment the line counter and reset the column.
		if ch == '\n' {
			currentLine++
			currentCol = 1
		} else {
			currentCol++
		}
	}

	return -1, -1 // not found.
}

// mdnDefPath returns the mozilla developer network CSS reference URL for the given css property.
func mdnDefPath(cssProperty string) string {
	return mdnCSSReferenceURL + cssProperty
}
