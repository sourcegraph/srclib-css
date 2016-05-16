package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/chris-ramon/net/html"

	cssParser "github.com/chris-ramon/douceur/parser"
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
			log.Printf("failed to read a source unit file: %s", err)
			continue
		}
		file := string(f)
		if isCSSFile(currentFile) {
			stylesheet, err := cssParser.Parse(file)
			if err != nil {
				return nil, err
			}
			for _, r := range stylesheet.Rules {
				for _, s := range r.Selectors {
					defStart, defEnd := findOffsets(file, s.Line, s.Column, s.Value)
					if defStart == 0 { // UI line highlighting doesn't work for graph.Def.DefStart = 0, remove this after fix the UI or other workaround.
						defStart = 1
					}
					out.Defs = append(out.Defs, &graph.Def{
						DefKey: graph.DefKey{
							UnitType: "Dir",
							Unit:     u.Name,
							Path:     s.Value,
						},
						Name:     s.Value,
						File:     filepath.ToSlash(currentFile),
						DefStart: uint32(defStart),
						DefEnd:   uint32(defEnd),
					})
				}
				declarations := r.Declarations
				for _, d := range declarations {
					s, e := findOffsets(file, d.Line, d.Column, d.Property)
					out.Refs = append(out.Refs, &graph.Ref{
						DefUnitType: "URL",
						DefUnit:     "MDN",
						DefPath:     mdnDefPath(d.Property),
						Unit:        u.Name,
						File:        filepath.ToSlash(currentFile),
						Start:       uint32(s),
						End:         uint32(e),
					})
				}
			}
		} else if isHTMLFile(currentFile) {
			z := html.NewTokenizer(strings.NewReader(file))
		L:
			for {
				tt := z.Next()
				switch tt {
				case html.ErrorToken:
					if z.Err() != io.EOF {
						return nil, z.Err()
					}
					break L
				case html.StartTagToken:
					t := z.Token()
					attrValSep := " "
					for _, attr := range t.Attr {
						prefix := ""
						if attr.Key == "id" {
							prefix = "#"
						} else if attr.Key == "class" {
							prefix = "."
						} else {
							continue
						}
						attrValues := strings.Split(attr.Val, attrValSep)

						var (
							// start and end are the byte offsets of one attribute value.
							// Which are re-calculated on each iteration of the next loop.
							start = uint32(attr.ValStart)
							end   uint32
						)
						for _, val := range attrValues {
							l := len([]byte(val))
							end = uint32(start + uint32(l))
							out.Refs = append(out.Refs, &graph.Ref{
								DefUnitType: "Dir",
								DefUnit:     u.Name,
								DefPath:     prefix + val,
								Unit:        u.Name,
								File:        filepath.ToSlash(currentFile),
								Start:       start,
								End:         end,
							})
							start = end + uint32(len(attrValSep))
						}
					}
				}
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
