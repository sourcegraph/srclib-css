package main

import (
	"bytes"
	"reflect"
	"sort"
	"testing"

	"github.com/chris-ramon/net/html"
)

func TestDescCombinatorSelectors(t *testing.T) {
	type testCase struct {
		expected []string
		node     html.Node
	}
	cases := []testCase{
		{
			expected: []string{
				"#app .container-inner",

				"#app #container-wrapper .container-inner",
				"#app #container-wrapper #container .container-inner",
				"#app #container-wrapper #container2 .container-inner",
				"#app #container-wrapper .section .container-inner",
				"#app #container-wrapper .col-xs-12 .container-inner",
				"#app #container-wrapper .center .container-inner",

				"#app .col-xs-6 .container-inner",
				"#app .col-xs-6 #container .container-inner",
				"#app .col-xs-6 #container2 .container-inner",
				"#app .col-xs-6 .section .container-inner",
				"#app .col-xs-6 .col-xs-12 .container-inner",
				"#app .col-xs-6 .center .container-inner",

				"#app #container .container-inner",
				"#app #container2 .container-inner",
				"#app .section .container-inner",
				"#app .col-xs-12 .container-inner",
				"#app .center .container-inner",

				"#container-wrapper .container-inner",
				"#container-wrapper #container .container-inner",
				"#container-wrapper #container2 .container-inner",
				"#container-wrapper .section .container-inner",
				"#container-wrapper .col-xs-12 .container-inner",
				"#container-wrapper .center .container-inner",

				".col-xs-6 .container-inner",
				".col-xs-6 #container .container-inner",
				".col-xs-6 #container2 .container-inner",
				".col-xs-6 .section .container-inner",
				".col-xs-6 .col-xs-12 .container-inner",
				".col-xs-6 .center .container-inner",

				"#container .container-inner",
				"#container2 .container-inner",
				".section .container-inner",
				".col-xs-12 .container-inner",
				".center .container-inner",
			},
			node: html.Node{
				Type: html.ElementNode,
				Attr: []html.Attribute{
					{Key: "class", Val: "container-inner"},
				},
				Parent: &html.Node{
					Type: html.ElementNode,
					Attr: []html.Attribute{
						{Key: "id", Val: "container container2"},
						{Key: "class", Val: "section col-xs-12 center"},
					},
					Parent: &html.Node{
						Type: html.ElementNode,
						Attr: []html.Attribute{
							{Key: "id", Val: "container-wrapper"},
							{Key: "class", Val: "col-xs-6"},
						},
						Parent: &html.Node{
							Type: html.ElementNode,
							Attr: []html.Attribute{
								{Key: "id", Val: "app"},
							},
						},
					},
				},
			},
		},
	}
	for i, c := range cases {
		got := DescCombinatorSelectors(&c.node)
		sort.Sort(bySelector(got))
		sort.Sort(bySelector(c.expected))
		if !reflect.DeepEqual(got, c.expected) {
			t.Fatalf("case: %d, got: %+v, expected: %+v", i, got, c.expected)
		}
	}
}

type bySelector []string

func (s bySelector) Len() int      { return len(s) }
func (s bySelector) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s bySelector) Less(i, j int) bool {
	switch bytes.Compare([]byte(s[i]), []byte(s[j])) {
	case -1:
		return true
	case 1:
		return false
	}
	return false
}
