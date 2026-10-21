package router

import "strings"

type trieNode struct {
	children map[string]*trieNode
	routes   []*Route
	prefix   string
}

func (n *trieNode) insert(prefix string, r *Route) {
	node := n
	for _, seg := range strings.Split(prefix, "/") {
		if seg == "" {
			continue
		}

		child, ok := node.children[seg]
		if !ok {
			if node.children == nil {
				node.children = make(map[string]*trieNode)
			}
			child = &trieNode{prefix: node.prefix + "/" + seg}
			node.children[seg] = child
		}
		node = child
	}

	node.routes = insertSorted(node.routes, r)
}

func (n *trieNode) longest(path string, req *requestView) (*Route, string) {
	var buf [16]*trieNode
	visited := buf[:0]

	node := n
	if len(node.routes) > 0 {
		visited = append(visited, node)
	}

	rest := path
	for node.children != nil {
		rest = strings.TrimLeft(rest, "/")
		if rest == "" {
			break
		}

		seg := rest
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			seg = rest[:i]
		}

		child, ok := node.children[seg]
		if !ok {
			break
		}

		node = child
		rest = rest[len(seg):]
		if len(node.routes) > 0 {
			visited = append(visited, node)
		}
	}

	for i := len(visited) - 1; i >= 0; i-- {
		if r := firstMatching(visited[i].routes, req); r != nil {
			return r, visited[i].prefix
		}
	}

	return nil, ""
}
