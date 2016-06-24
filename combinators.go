package main

import (
	"fmt"
	"strings"

	"github.com/chris-ramon/net/html"
)

type CombinatorFn func(sn selNode) []string

// DescCombinatorSelectors walks upwards given node, then builds and returns all its possible descendant combinator selectors.
func DescCombinatorSelectors(sn selNode) []string {
	var (
		selectors   []selNode
		currentNode *html.Node = sn.node
		attrValSep  string     = " "
	)
	for {
		if currentNode.Type == html.ElementNode {
			for _, attr := range currentNode.Attr {
				if attr.Key != "id" && attr.Key != "class" {
					continue
				}
				attrValues := strings.Split(attr.Val, attrValSep)
				for _, val := range attrValues {
					selStr := selPrefix(attr.Key) + val
					if currentNode == sn.node {
						continue
					}
					for _, s := range selectors {
						if s.node != currentNode {
							selectors = append(selectors, selNode{
								sel:  *newSelector(fmt.Sprintf("%s %s", selStr, s.sel.String())),
								node: currentNode,
							})
						}
					}
					selectors = append(selectors, selNode{
						sel:  *newSelector(fmt.Sprintf("%s %s", selStr, sn.sel.String())),
						node: currentNode,
					})
				}
			}
		}
		if currentNode.Parent == nil {
			break
		}
		currentNode = currentNode.Parent
	}
	var result []string
	for _, s := range selectors {
		result = append(result, s.sel.String())
	}
	return result
}

// ChildCombinatorSelectors walks upwards given node, then builds and returns all its possible child combinator selectors.
func ChildCombinatorSelectors(sn selNode) []string {
	var (
		selectors   []selNode
		currentNode *html.Node = sn.node
		attrValSep  string     = " "
	)
	for {
		if currentNode.Type == html.ElementNode {
			for _, attr := range currentNode.Attr {
				if attr.Key != "id" && attr.Key != "class" {
					continue
				}
				attrValues := strings.Split(attr.Val, attrValSep)
				for _, val := range attrValues {
					selStr := selPrefix(attr.Key) + val
					if currentNode == sn.node {
						continue
					}
					for _, s := range selectors {
						if s.node != currentNode && s.node.Parent == currentNode {
							selectors = append(selectors, selNode{
								sel:  *newSelector(fmt.Sprintf("%s > %s", selStr, s.sel.String())),
								node: currentNode,
							})
						}
					}
					if sn.node.Parent == currentNode {
						selectors = append(selectors, selNode{
							sel:  *newSelector(fmt.Sprintf("%s > %s", selStr, sn.sel.String())),
							node: currentNode,
						})
					}
				}
			}
		}
		if currentNode.Parent == nil {
			break
		}
		currentNode = currentNode.Parent
	}
	var result []string
	for _, s := range selectors {
		result = append(result, s.sel.String())
	}
	return result
}

// AdjacentCombinatorSelectors builds and returns adjacent sibling combinator selectors for given selectors node.
func AdjacentCombinatorSelectors(sn selNode) []string {
	var (
		selectors  []string
		prev       *html.Node = sn.node.PrevSibling
		attrValSep string     = " "
	)
	if prev == nil {
		return selectors
	}
	for _, attr := range prev.Attr {
		attrValues := strings.Split(attr.Val, attrValSep)
		for _, val := range attrValues {
			selStr := selPrefix(attr.Key) + val
			sel := fmt.Sprintf("%s + %s", selStr, sn.sel.String())
			selectors = append(selectors, sel)
		}
	}
	return selectors
}

// GeneralCombinatorSelectors builds and returns general sibling combinator selectors for given selectors node.
func GeneralCombinatorSelectors(sn selNode) []string {
	var (
		attrValSep string     = " "
		prev       *html.Node = sn.node.PrevSibling
		selectors  []string
	)
	if prev == nil {
		return selectors
	}
	for _, attr := range prev.Attr {
		attrValues := strings.Split(attr.Val, attrValSep)
		for _, val := range attrValues {
			selStr := selPrefix(attr.Key) + val
			sel := fmt.Sprintf("%s ~ %s", selStr, sn.sel.String())
			selectors = append(selectors, sel)
		}
	}
	return selectors
}
