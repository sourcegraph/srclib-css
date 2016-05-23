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
	"regexp"
	"strings"

	"github.com/chris-ramon/douceur/css"
	"github.com/chris-ramon/net/html"
	"github.com/sourcegraph/srclib-css/css_def"

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

	o, err := doGraph(u)
	if err != nil {
		return nil, err
	}

	return o, nil
}

// selector represents either a single selector or a chain of selectors.
type selector string

// selectors represents a group of selectors used to keep track uniqueness.
type selectors map[selector]bool

// addSelector & selectorExists represents functions that perform actions on `selectors` type.
type addSelector func(s selector)
type selectorExists func(s selector) bool

var lastSelectorRegexp = regexp.MustCompile(".*([\\.\\#].+)")

func selSplitFn(r rune) bool {
	return r == '>' || r == '+' || r == '~'
}

// lastSelector returns the last selector from given selector chain.
func lastSelector(s string) *selector {
	// Splits `s` into a slice of selectors. It might be a single selector or a chain of selectors(Eg. ".panel > .panel-body + .table").
	selectors := strings.FieldsFunc(s, selSplitFn)

	// sel is the single or last selector from selectors chain.
	sel := strings.TrimSpace(selectors[len(selectors)-1])

	// sel might still be a chain of selectors(Eg. "h1.title")
	// `lastSelectorRegexp` is used to obtain the last selector which starts either with "." or "#".
	m := lastSelectorRegexp.FindStringSubmatch(sel)
	if len(m) != 2 {
		return nil
	}
	var l selector
	l = selector(m[1])
	return &l
}

func doGraph(u *unit.SourceUnit) (*graph.Output, error) {
	out := graph.Output{}

	// At the moment we support a unique selector per `out`.
	var (
		// sels are used to keep track which selectors exists for `out`.
		sels selectors = make(selectors, 0)

		// selExists is used to check if a given selector exits in `sels`.
		selExists selectorExists

		// addSel is used to add a given selector to `sels`.
		addSel addSelector
	)
	selExists = func(s selector) bool {
		if _, found := sels[s]; found {
			return true
		}
		return false
	}
	addSel = func(s selector) {
		sels[s] = true
	}

	// Iterate over u.Files, for each file the process performed can be described as follow:
	// 1. Read the file and parse its data.
	// 2. For CSS files: for each selector found a graph.Def is created and for each property of that selector a graph.Ref is created.
	// 3. For HTML files: for each tag attribute(id and class) a graph.Ref is created.
	for _, f := range u.Files {
		fileBytes, err := ioutil.ReadFile(f)
		if err != nil {
			log.Printf("failed to read a source unit file: %s", err)
			continue
		}
		data := string(fileBytes)
		if isCSSFile(f) {
			stylesheet, err := cssParser.Parse(data)
			if err != nil {
				log.Printf("failed to parse a source unit file: %s", err)
				continue
			}
			for _, r := range stylesheet.Rules {
				defs, err := getCSSDefs(u, data, f, r, selExists, addSel)
				if err != nil {
					return nil, err
				}
				out.Defs = append(out.Defs, defs...)
				out.Refs = append(out.Refs, getCSSRefs(u, data, f, r)...)
			}
		} else if isHTMLFile(f) {
			refs, err := getHTMLRefs(u, data, f)
			if err != nil {
				return nil, err
			}
			out.Refs = append(out.Refs, refs...)
		}
	}
	return &out, nil
}

func getCSSDefs(u *unit.SourceUnit, data string, filePath string, r *css.Rule, selExists selectorExists, addSel addSelector) ([]*graph.Def, error) {
	defs := []*graph.Def{}
	for _, s := range r.Selectors {
		defStart, defEnd := findOffsets(data, s.Line, s.Column, s.Value)

		// TODO (chris): remove this when frontend is improved to handle this case.
		if defStart == 0 { // UI line highlighting doesn't work for graph.Def.DefStart = 0, remove this after fix the UI or other workaround.
			defStart = 1
		}

		// Obtains last selector from the selectors chain `s.Value`.
		sel := lastSelector(s.Value)
		if sel == nil {
			continue
		}

		// Current implementation supports a unique selector per graph.Output.
		if selExists(*sel) {
			continue
		}
		addSel(*sel)

		selStr := string(*sel)
		d, err := json.Marshal(css_def.DefData{
			Keyword: "selector",
			Kind:    selectorKind(selStr),
		})
		if err != nil {
			return defs, err
		}
		defs = append(defs, &graph.Def{
			DefKey: graph.DefKey{
				UnitType: "basic-css",
				Unit:     u.Name,
				Path:     selStr,
			},
			Name:     selStr,
			File:     filepath.ToSlash(filePath),
			DefStart: uint32(defStart),
			DefEnd:   uint32(defEnd),
			Data:     d,
		})
	}
	return defs, nil
}

func getCSSRefs(u *unit.SourceUnit, data string, filePath string, r *css.Rule) []*graph.Ref {
	refs := []*graph.Ref{}
	for _, d := range r.Declarations {
		s, e := findOffsets(data, d.Line, d.Column, d.Property)
		refs = append(refs, &graph.Ref{
			DefUnitType: "URL",
			DefUnit:     "MDN",
			DefPath:     mdnDefPath(d.Property),
			Unit:        u.Name,
			File:        filepath.ToSlash(filePath),
			Start:       uint32(s),
			End:         uint32(e),
		})
	}
	return refs
}

func getHTMLRefs(u *unit.SourceUnit, data string, filePath string) ([]*graph.Ref, error) {
	refs := []*graph.Ref{}
	z := html.NewTokenizer(strings.NewReader(data))
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
					refs = append(refs, &graph.Ref{
						DefUnitType: "basic-css",
						DefUnit:     u.Name,
						DefPath:     prefix + val,
						Unit:        u.Name,
						File:        filepath.ToSlash(filePath),
						Start:       start,
						End:         end,
					})
					start = end + uint32(len(attrValSep))
				}
			}
		}
	}
	return refs, nil
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

var vendorPrefixRegExp = regexp.MustCompile("^-webkit-|^-moz-|^-ms-|^-o-")

// mdnDefPath returns the mozilla developer network CSS reference URL for the given css property.
func mdnDefPath(cssProperty string) string {
	if strings.HasPrefix(cssProperty, "-webkit-") || strings.HasPrefix(cssProperty, "-moz-") || strings.HasPrefix(cssProperty, "-ms-") || strings.HasPrefix(cssProperty, "-o-") {
		return mdnCSSReferenceURL + vendorPrefixRegExp.ReplaceAllString(cssProperty, "")
	}
	return mdnCSSReferenceURL + cssProperty
}

func selectorKind(selectorStr string) string {
	if strings.HasPrefix(selectorStr, "#") {
		return "id"
	} else if strings.HasPrefix(selectorStr, ".") {
		return "class"
	}
	return ""
}
