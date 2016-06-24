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

// selNode represents a selector HTML node.
type selNode struct {
	sel  selector
	node *html.Node
}

// descSelectorRegexp is a regexp pattern that matches individual selectors from a descendant selector, Eg. "h1.title" matches "h1" and "title".
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
					defs, refs, err := cssDefsAndRefs(*s, u, data, f, r, defExist)
					if err != nil {
						return nil, err
					}
					out.Defs = append(out.Defs, defs...)
					out.Refs = append(out.Refs, refs...)
				}
				out.Refs = append(out.Refs, mdnCSSRefs(u, data, f, r)...)
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

// selectorDefsAndRefs returns defs and refs for CSS selectors present on given CSS rule.
func cssDefsAndRefs(s css.Selector, u *unit.SourceUnit, data string, filePath string, r *css.Rule, defExist selectorDefExist) ([]*graph.Def, []*graph.Ref, error) {
	defs := []*graph.Def{}
	refs := []*graph.Ref{}

	defStart, defEnd := findOffsets(data, s.Line, s.Column, s.Value)

	// TODO (chris): remove this when frontend is improved to handle this case.
	if defStart == 0 { // UI line highlighting doesn't work for graph.Def.DefStart = 0, remove this after fix the UI or other workaround.
		defStart = 1
	}

	d, err := json.Marshal(css_def.DefData{
		Keyword: "selector",
		Kind:    selectorKind(s.Value),
	})
	if err != nil {
		return nil, nil, err
	}
	def := &graph.Def{
		DefKey: graph.DefKey{
			UnitType: "basic-css",
			Unit:     u.Name,
			Path:     selectorDefPath(filePath, *newSelector(s.Value)),
		},
		Name:     s.Value,
		File:     filepath.ToSlash(filePath),
		DefStart: uint32(defStart),
		DefEnd:   uint32(defEnd),
		Data:     d,
	}

	// Checks if a CSS selectors definition already exists; if so, it should not be added.
	// Currenttly `srclib-css` emits only one `graph.Def` per CSS selector.
	if defExist(def) {
		return nil, nil, nil
	}
	defs = append(defs, def)

	refs = append(refs, &graph.Ref{
		DefUnitType: def.DefKey.UnitType,
		DefUnit:     def.DefKey.Unit,
		DefPath:     def.DefKey.Path,
		Unit:        def.DefKey.Unit,
		File:        def.File,
		Def:         true,
		Start:       def.DefStart,
		End:         def.DefEnd,
	})

	return defs, refs, nil
}

// mdnCSSRefs returns refs to MDN(Mozilla Developer Network) for CSS properties present on given CSS rule.
func mdnCSSRefs(u *unit.SourceUnit, data string, filePath string, r *css.Rule) []*graph.Ref {
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

// htmlRefs returns refs for CSS selectors present on given HTML data.
func htmlRefs(u *unit.SourceUnit, data string, filePath string, selectorDefs []*graph.Def) ([]*graph.Ref, error) {
	refs := []*graph.Ref{}

	// linkTagsZ is a HTML tokenizer used to search for `<link rel="stylesheet" ...>` tags.
	linkTagsZ := html.NewTokenizer(strings.NewReader(data))

	// stylesheetHREFs is a slice which contains all the stylesheet HREFs found defined on HTML `data`.
	var stylesheetHREFs = []string{}

	// Search for all stylesheet tags defined on `data`, then save its HREFs on `stylesheetHREFs` for later usage.
	// This step is not realized on `LtagsZ`s loop because link tags might be defined outside head tag, therefore
	// we cannot rely on assuming all link tags are read before regular HTML tags.
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
					stylesheetHREFs = append(stylesheetHREFs, normalizeStylesheetHREF(href, filepath.Dir(filePath)))
				}
			}
		}
	}

	defs := filterDefs(selectorDefs, func(def *graph.Def) bool {
		for _, f := range stylesheetHREFs {
			if def.File == f {
				return true
			}
		}
		return false
	})

	// Not defs were found for given HTML file `filePath`.
	if len(defs) == 0 {
		return nil, nil
	}

	doc, err := html.Parse(strings.NewReader(data))
	if err != nil {
		return nil, err
	}

	var (
		walk       func(*html.Node)
		attrValSep string = " "
	)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, attr := range n.Attr {
				if attr.Key != "id" && attr.Key != "class" {
					continue
				}
				// `attr.Val` might contain multiple CSS selectors. Eg. `class="btn btn-sm"`
				attrValues := strings.Split(attr.Val, attrValSep)
				var (
					// start and end are the byte offsets of one attribute value.
					// Which are re-calculated on each iteration of the next current loop-iteration.
					start = uint32(attr.ValStart)
					end   uint32
				)
				for _, val := range attrValues {
					l := len([]byte(val))
					end = uint32(start + uint32(l))
					def := SelectorDef(defs, selNode{
						sel:  *newSelector(selPrefix(attr.Key) + val),
						node: n,
					})
					if def == nil { // selector definition not found.
						start = end + uint32(len(attrValSep))
						continue
					}
					refs = append(refs, &graph.Ref{
						DefUnitType: "basic-css",
						DefUnit:     u.Name,
						DefPath:     def.DefKey.Path,
						Unit:        u.Name,
						File:        filepath.ToSlash(filePath),
						Start:       start,
						End:         end,
					})
					start = end + uint32(len(attrValSep))
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
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

// selectorDef searches for a definition which represents the given selector.
func SelectorDef(defs []*graph.Def, sel selNode) *graph.Def {
	for _, def := range defs {
		if def.Name == sel.sel.String() {
			return def
		}
	}
	var (
		combSelectors []string
		combFns       []CombinatorFn = []CombinatorFn{
			DescCombinatorSelectors,
			ChildCombinatorSelectors,
			AdjacentCombinatorSelectors,
			GeneralCombinatorSelectors,
		}
	)
	for _, combFn := range combFns {
		combSelectors = append(combSelectors, combFn(sel)...)
	}
	for _, def := range defs {
		for _, cs := range combSelectors {
			if def.Name == cs {
				return def
			}
		}
	}
	return nil
}

// normalizeStylesheetHREF normalizes given `stylesheetHREFs` to be relative path of given `root`.
func normalizeStylesheetHREF(stylesheetHREF string, root string) string {
	return filepath.ToSlash(filepath.Join(root, stylesheetHREF))
}

// selPrefix checks given HTML attribute and returns either `#` or `.`.
func selPrefix(attr string) string {
	switch attr {
	case "id":
		return "#"
	case "class":
		return "."
	}
	return ""
}

// filterDefs filter given defs using given predicate as filtering function.
func filterDefs(defs []*graph.Def, predicate func(def *graph.Def) bool) []*graph.Def {
	var filteredDefs []*graph.Def
	for _, def := range defs {
		if ok := predicate(def); ok {
			filteredDefs = append(filteredDefs, def)
		}
	}
	return filteredDefs
}
