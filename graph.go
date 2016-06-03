package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/chris-ramon/douceur/css"
	"github.com/chris-ramon/net/html"
	"sourcegraph.com/sourcegraph/srclib-css/css_def"

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

// selector represents a CSS selector, which might be:
// - Comma separated selectors chain, Eg. ".panel, .panel-body, .panel-info".
// - Descendant selector, Eg. "h1.title".
// - Single selector, Eg. "#container".
type selector string

func (s *selector) String() string {
	return string(*s)
}

func newSelector(sel string) *selector {
	s := selector(sel)
	return &s
}

// descSelectorRegexp is a regexp pattern that matches individual selectors from a descendant selector, Eg. "h1.title".
var descSelectorRegexp = regexp.MustCompile(".*([\\.\\#].+)")

// selSplitFn returns true if given CSS combinator is valid.
func selSplitFn(combinator rune) bool {
	return combinator == '>' || combinator == '+' || combinator == '~'
}

// lastSelector returns last selector from given selectors chain.
func lastSelector(s string) *selector {
	selectors := strings.FieldsFunc(s, selSplitFn)
	lastSel := strings.TrimSpace(selectors[len(selectors)-1])
	matches := descSelectorRegexp.FindStringSubmatch(lastSel)
	if len(matches) != 2 {
		return nil
	}
	lastSelElement := matches[1]
	return newSelector(lastSelElement)
}

// selectorDefExist returns true if given `def` exists in a set of definitions.
type selectorDefExist func(def *graph.Def) bool

func doGraph(u *unit.SourceUnit) (*graph.Output, error) {
	out := graph.Output{}

	var defExist selectorDefExist
	defExist = func(def *graph.Def) bool {
		for _, d := range out.Defs {
			if d.Name == def.Name && d.DefKey.Path == def.DefKey.Path {
				return true
			}
		}
		return false
	}

	// For each CSS file on `u.Files`:
	// - Create a `graph.Def` for each CSS selector.
	// - Create a `graph.Ref` for each CSS selector property.
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
				// `r.Selectors` is either a single selector or a selectors chain.
				for _, s := range r.Selectors {
					if s.Value == "" {
						// If `s.Value` is an empty selector, might be due to malformed CSS syntax.
						log.Printf("unexpected empty selector, rules: %+v", stylesheet.Rules)
						continue
					}
					defs, err := cssDefs(*s, u, data, f, r, defExist)
					if err != nil {
						return nil, err
					}
					out.Defs = append(out.Defs, defs...)
				}
				out.Refs = append(out.Refs, cssRefs(u, data, f, r)...)
			}
		}
	}

	// For each HTML file on `u.Files`:
	// - Create a `graph.Ref` for each element within each HTML tag id/class attribute, which points to an existing `graph.Def` previously created.
	for _, f := range u.Files {
		fileBytes, err := ioutil.ReadFile(f)
		if err != nil {
			log.Printf("failed to read a source unit file: %s", err)
			continue
		}
		data := string(fileBytes)
		if isHTMLFile(f) {
			refs, err := htmlRefs(u, data, f, out.Defs)
			if err != nil {
				return nil, err
			}
			out.Refs = append(out.Refs, refs...)
		}
	}

	return &out, nil
}

func cssDefs(s css.Selector, u *unit.SourceUnit, data string, filePath string, r *css.Rule, defExist selectorDefExist) ([]*graph.Def, error) {
	defs := []*graph.Def{}
	defStart, defEnd := findOffsets(data, s.Line, s.Column, s.Value)

	// TODO (chris): remove this when frontend is improved to handle this case.
	if defStart == 0 { // UI line highlighting doesn't work for graph.Def.DefStart = 0, remove this after fix the UI or other workaround.
		defStart = 1
	}

	// Obtain last selector from a selectors chain.
	sel := lastSelector(s.Value)
	if sel == nil {
		return nil, nil
	}

	selStr := string(*sel)
	d, err := json.Marshal(css_def.DefData{
		Keyword: "selector",
		Kind:    selectorKind(selStr),
	})
	if err != nil {
		return nil, err
	}
	def := &graph.Def{
		DefKey: graph.DefKey{
			UnitType: "basic-css",
			Unit:     u.Name,
			Path:     selectorDefPath(filePath, *sel),
		},
		Name:     selStr,
		File:     filepath.ToSlash(filePath),
		DefStart: uint32(defStart),
		DefEnd:   uint32(defEnd),
		Data:     d,
	}

	// Checks if `def.Name`(CSS selector) already exists; if so, it should not be added, current implementation supports
	// one `graph.Def` per one selector.
	if defExist(def) {
		return nil, nil
	}
	defs = append(defs, def)
	return defs, nil
}

func cssRefs(u *unit.SourceUnit, data string, filePath string, r *css.Rule) []*graph.Ref {
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

func htmlRefs(u *unit.SourceUnit, data string, filePath string, selectorDefs []*graph.Def) ([]*graph.Ref, error) {
	refs := []*graph.Ref{}

	// linkTagsZ is a HTML tokenizer used to search for `<link rel="stylesheet" ...>` tags.
	linkTagsZ := html.NewTokenizer(strings.NewReader(data))

	// stylesheetHREFs is a slice which contains all the stylesheet HREFs found defined on HTML file `z`.
	var stylesheetHREFs = []string{}

	// Search for all the style tags defined on `data` to extract its HREFs to be later use, when resolving
	// the definition path of CSS selector found on HTML tags id/class attributes.
	// Not looking for style tags inside `LtagsZ`: To make sure all stylesheet HREFs are found first:
	// `<link ...>` tags might be placed in different locations in the HTML document.
LlinkTags:
	for {
		tt := linkTagsZ.Next()
		switch tt {
		case html.ErrorToken:
			if linkTagsZ.Err() != io.EOF {
				return nil, linkTagsZ.Err()
			}
			break LlinkTags
		case html.StartTagToken, html.SelfClosingTagToken:
			t := linkTagsZ.Token()
			if t.Data == "link" {
				isStylesheetLink := false
				href := ""
				for _, attr := range t.Attr {
					if attr.Key == "href" {
						href = attr.Val
					}
					if attr.Key == "rel" && attr.Val == "stylesheet" {
						isStylesheetLink = true
					}
				}
				if isStylesheetLink {
					stylesheetHREFs = append(stylesheetHREFs, href)
				}
			}
		}
	}

	tagsZ := html.NewTokenizer(strings.NewReader(data))
LtagsZ:
	for {
		tt := tagsZ.Next()
		switch tt {
		case html.ErrorToken:
			if tagsZ.Err() != io.EOF {
				return nil, tagsZ.Err()
			}
			break LtagsZ
		case html.StartTagToken, html.SelfClosingTagToken:
			t := tagsZ.Token()
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
					hrefs := normalizeStylesheetHREFs(stylesheetHREFs, filepath.Dir(filePath))
					defPath := resolveSelectorDefPath(selectorDefs, selector(prefix+val), hrefs)
					if defPath == nil { // selector definition not found.
						continue
					}
					refs = append(refs, &graph.Ref{
						DefUnitType: "basic-css",
						DefUnit:     u.Name,
						DefPath:     *defPath,
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

// selectorDefPath returns the def path of the given selector.
func selectorDefPath(filePath string, s selector) string {
	return fmt.Sprintf("%s%s", filepath.ToSlash(filePath), string(s))
}

// resolveSelectorDefPath returns the definition path of given selector(`s`).
func resolveSelectorDefPath(selectorsDef []*graph.Def, s selector, stylesheetPaths []string) *string {
	for _, def := range selectorsDef {
		if def.Name == s.String() && stylesheetPathExists(stylesheetPaths, def.File) {
			return &def.DefKey.Path
		}
	}
	return nil
}

// normalizeStylesheetHREFs normalizes each element of given `stylesheetHREFs`
// to be relative path of given `root`.
func normalizeStylesheetHREFs(stylesheetHREFs []string, root string) []string {
	var normalized []string
	for _, s := range stylesheetHREFs {
		normalized = append(normalized, filepath.ToSlash(filepath.Join(root, s)))
	}
	return normalized
}

// stylesheetPathExists returns true if given filepath(`fp`) exists on `stylesheetPaths`.
func stylesheetPathExists(stylesheetsPath []string, fp string) bool {
	for _, s := range stylesheetsPath {
		if s == fp {
			return true
		}
	}
	return false
}
