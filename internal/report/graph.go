package report

import (
	"cmp"
	"fmt"
	"math/big"
	"slices"

	"walletflow/internal/address"
	"walletflow/internal/ledger"
)

// GraphNode is a wallet, an entity (addresses sharing a name) or an unknown counterparty.
type GraphNode struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Kind      string   `json:"kind"` // mine | exchange | external | watch | unknown | shared | more
	Color     string   `json:"color,omitempty"`
	Ref       string   `json:"ref,omitempty"` // value for the transactions filter
	Addresses []string `json:"addresses,omitempty"`
	Count     int      `json:"count"`
}

// AssetTotal is how much of one asset moved along an edge.
type AssetTotal struct {
	Symbol string  `json:"symbol"`
	Chain  string  `json:"chain"`
	Amount string  `json:"amount"`
	Value  float64 `json:"value"`
}

// GraphEdge aggregates all transfers from one node to another.
type GraphEdge struct {
	ID      string       `json:"id"`
	Source  string       `json:"source"`
	Target  string       `json:"target"`
	Count   int          `json:"count"`
	Class   string       `json:"class"` // most frequent class
	Totals  []AssetTotal `json:"totals"`
	First   int64        `json:"first"`
	Last    int64        `json:"last"`
	Unknown bool         `json:"unknown,omitempty"`
}

// Graph is the data for the connections view.
type Graph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
	// HiddenUnknown is how many unknown counterparties were folded into "more" nodes.
	HiddenUnknown int `json:"hiddenUnknown"`
}

// GraphOptions tune BuildGraph.
type GraphOptions struct {
	Group        bool  // merge addresses with the same name into one node
	UnknownIn    bool  // show senders outside the book
	UnknownOut   bool  // show receivers outside the book
	UnknownSince int64 // only transfers since this time count for UnknownIn/Out
	MaxUnknown   int   // unknown counterparties shown per direction, the rest are folded
	// Common shows every outside address linked to two or more book nodes,
	// whatever the window and the limit: the way to see how wallets are connected.
	Common bool
}

// BuildGraph links address book entries by the transfers between them.
// ts may hold any transfers touching the book; the rest is decided by opt.
func BuildGraph(ts []ledger.Transfer, book ledger.Book, opt GraphOptions) Graph {
	b := graphBuilder{book: book, opt: opt, nodes: map[string]*GraphNode{}, edges: map[[2]string]*edgeAcc{}}
	var outside []ledger.Transfer
	for _, t := range ts {
		_, fromKnown := book[t.From]
		_, toKnown := book[t.To]
		switch {
		case fromKnown && toKnown:
			b.add(b.bookNode(t.From), b.bookNode(t.To), t, false)
		case fromKnown || toKnown:
			outside = append(outside, t)
		}
	}
	b.addOutside(outside)
	// Synced wallets always show, even without links: "no connection" is an answer too.
	for addr, a := range book {
		if a.Kind.Synced() {
			b.bookNode(addr)
		}
	}
	return b.result()
}

type edgeAcc struct {
	GraphEdge
	classes map[ledger.Class]int
	sums    map[[2]string]*big.Int
	assets  map[[2]string]ledger.Asset
}

type graphBuilder struct {
	book   ledger.Book
	opt    GraphOptions
	nodes  map[string]*GraphNode
	edges  map[[2]string]*edgeAcc
	hidden int
}

func (b *graphBuilder) node(id string, mk func() GraphNode) *GraphNode {
	n := b.nodes[id]
	if n == nil {
		v := mk()
		n = &v
		b.nodes[id] = n
	}
	return n
}

func (b *graphBuilder) nodeID(addr string) string {
	if a := b.book[addr]; b.opt.Group && a.Name != "" {
		return "n:" + a.Name
	}
	return "a:" + addr
}

func (b *graphBuilder) bookNode(addr string) *GraphNode {
	a := b.book[addr]
	id := b.nodeID(addr)
	n := b.node(id, func() GraphNode {
		ref := addr
		if b.opt.Group && a.Name != "" {
			ref = a.Name
		}
		return GraphNode{ID: id, Label: a.Label(), Kind: string(a.Kind), Color: a.Color, Ref: ref}
	})
	if !slices.Contains(n.Addresses, addr) {
		n.Addresses = append(n.Addresses, addr)
	}
	if n.Color == "" {
		n.Color = a.Color
	}
	return n
}

func (b *graphBuilder) add(from, to *GraphNode, t ledger.Transfer, unknown bool) {
	if from.ID == to.ID {
		return // moves inside one entity
	}
	k := [2]string{from.ID, to.ID}
	e := b.edges[k]
	if e == nil {
		e = &edgeAcc{
			GraphEdge: GraphEdge{ID: fmt.Sprintf("e%d", len(b.edges)), Source: from.ID, Target: to.ID, First: t.TS, Last: t.TS, Unknown: unknown},
			classes:   map[ledger.Class]int{},
			sums:      map[[2]string]*big.Int{},
			assets:    map[[2]string]ledger.Asset{},
		}
		b.edges[k] = e
	}
	e.Count++
	e.First, e.Last = min(e.First, t.TS), max(e.Last, t.TS)
	e.classes[t.Class]++
	ak := [2]string{t.Asset.Chain, t.Asset.Contract}
	if amt, ok := new(big.Int).SetString(t.AmountRaw, 10); ok {
		if e.sums[ak] == nil {
			e.sums[ak] = new(big.Int)
			e.assets[ak] = t.Asset
		}
		e.sums[ak].Add(e.sums[ak], amt)
	}
	from.Count++
	to.Count++
}

// addOutside adds addresses outside the book: shared ones (Common), then the
// most active ones per direction within the window; the rest is folded.
func (b *graphBuilder) addOutside(ts []ledger.Transfer) {
	type side struct {
		incoming bool // money goes from the outside address into the book
		addr     string
	}
	// bookSide returns the book address of t and whether t comes into the book.
	bookSide := func(t ledger.Transfer) (string, side) {
		if _, ok := b.book[t.To]; ok {
			return t.To, side{true, t.From}
		}
		return t.From, side{false, t.To}
	}

	shared := map[string]bool{}
	if b.opt.Common {
		links := map[string]map[string]bool{} // outside address → book node ids
		for _, t := range ts {
			own, s := bookSide(t)
			if links[s.addr] == nil {
				links[s.addr] = map[string]bool{}
			}
			links[s.addr][b.nodeID(own)] = true
		}
		for addr, nodes := range links {
			if len(nodes) >= 2 {
				shared[addr] = true
			}
		}
	}

	inWindow := func(t ledger.Transfer, s side) bool {
		return t.TS >= b.opt.UnknownSince && (s.incoming && b.opt.UnknownIn || !s.incoming && b.opt.UnknownOut)
	}
	counts := map[side]int{}
	for _, t := range ts {
		if _, s := bookSide(t); !shared[s.addr] && inWindow(t, s) {
			counts[s]++
		}
	}
	keep := map[side]bool{}
	for _, incoming := range []bool{true, false} {
		var list []side
		for s := range counts {
			if s.incoming == incoming {
				list = append(list, s)
			}
		}
		slices.SortFunc(list, func(a, b side) int {
			return cmp.Or(cmp.Compare(counts[b], counts[a]), cmp.Compare(a.addr, b.addr))
		})
		for i, s := range list {
			if b.opt.MaxUnknown <= 0 || i < b.opt.MaxUnknown {
				keep[s] = true
			} else {
				b.hidden++
			}
		}
	}

	for _, t := range ts {
		own, s := bookSide(t)
		var un *GraphNode
		switch {
		case shared[s.addr]:
			un = b.outsideNode(s.addr, "shared")
		case !inWindow(t, s):
			continue
		case keep[s]:
			un = b.outsideNode(s.addr, "unknown")
		default:
			id, label := "more:out", "другие получатели"
			if s.incoming {
				id, label = "more:in", "другие отправители"
			}
			un = b.node(id, func() GraphNode { return GraphNode{ID: id, Label: label, Kind: "more"} })
		}
		me := b.bookNode(own)
		if s.incoming {
			b.add(un, me, t, true)
		} else {
			b.add(me, un, t, true)
		}
	}
}

func (b *graphBuilder) outsideNode(addr, kind string) *GraphNode {
	return b.node("u:"+addr, func() GraphNode {
		return GraphNode{ID: "u:" + addr, Label: address.Short(addr), Kind: kind, Ref: addr, Addresses: []string{addr}}
	})
}

func (b *graphBuilder) result() Graph {
	g := Graph{HiddenUnknown: b.hidden}
	for _, n := range b.nodes {
		if n.Kind == "more" {
			n.Label = fmt.Sprintf("%s (%d)", n.Label, b.hidden)
		}
		g.Nodes = append(g.Nodes, *n)
	}
	slices.SortFunc(g.Nodes, func(a, b GraphNode) int { return cmp.Compare(a.ID, b.ID) })
	for _, e := range b.edges {
		best := 0
		for c, n := range e.classes {
			if n > best || n == best && string(c) < e.Class {
				best, e.Class = n, string(c)
			}
		}
		for k, sum := range e.sums {
			a := e.assets[k]
			raw := sum.String()
			e.Totals = append(e.Totals, AssetTotal{Symbol: a.Symbol, Chain: a.Chain,
				Amount: ledger.FormatAmount(raw, a.Decimals), Value: ledger.AmountFloat(raw, a.Decimals)})
		}
		slices.SortFunc(e.Totals, func(a, b AssetTotal) int { return cmp.Compare(b.Value, a.Value) })
		g.Edges = append(g.Edges, e.GraphEdge)
	}
	slices.SortFunc(g.Edges, func(a, b GraphEdge) int { return cmp.Compare(b.Count, a.Count) })
	return g
}
