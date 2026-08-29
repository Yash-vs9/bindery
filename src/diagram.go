package main

import (
	"fmt"
	"strings"
)

// Flowchart diagrams.
//
// A ```mermaid fence becomes an inline SVG. This replaces mermaid, which is
// several hundred kilobytes of JavaScript that most documentation sites load on
// every page in order to draw a handful of boxes and arrows.
//
// There are three parts: a parser for the subset of mermaid's flowchart syntax
// that people actually write, a layered layout, and an SVG emitter. SVG is text,
// so no image library is involved -- the whole thing is arithmetic and string
// building, like the PDF writer.
//
// The layout is layered, in the tradition of Sugiyama: assign each node to a
// layer by longest path from a root, order nodes within each layer to reduce
// edge crossings, then assign coordinates. It is the simplified version -- one
// ordering heuristic rather than an iterated one, and straight edges rather than
// splines -- and the README says where that shows.
//
// Box sizes come from the Helvetica advance widths in pdfmetrics.go, and the
// emitted SVG asks for Helvetica. Measuring in the font that will actually be
// used is why labels fit their boxes instead of approximately fitting them.

// diagramDirection is the flow of the graph.
type diagramDirection int

const (
	flowDown diagramDirection = iota
	flowRight
)

// nodeShape is the outline a node is drawn with.
type nodeShape int

const (
	shapeRect nodeShape = iota
	shapeRound
	shapeDiamond
	shapeCircle
)

type diagramNode struct {
	id    string
	label string
	shape nodeShape

	layer int
	order int
	x, y  float64
	w, h  float64
}

type diagramEdge struct {
	from, to *diagramNode
	label    string
	dashed   bool
}

type diagram struct {
	direction diagramDirection
	nodes     []*diagramNode
	byID      map[string]*diagramNode
	edges     []diagramEdge
}

// Layout constants, in SVG user units.
const (
	diagramFontSize   = 13.0
	diagramNodeHeight = 38.0
	diagramPadX       = 18.0
	diagramMinWidth   = 56.0
	diagramLayerGap   = 62.0
	diagramNodeGap    = 26.0
	diagramMargin     = 12.0
)

// parseDiagram reads the flowchart subset. It returns nil when the source is
// not a diagram this understands, so that an unrecognised fence falls back to
// being rendered as ordinary code rather than as an error.
func parseDiagram(src string) *diagram {
	lines := strings.Split(src, "\n")
	d := &diagram{byID: map[string]*diagramNode{}}

	header := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "%%") {
			continue // blank, or a mermaid comment
		}

		if !header {
			fields := strings.Fields(line)
			if len(fields) == 0 || (fields[0] != "graph" && fields[0] != "flowchart") {
				return nil
			}
			if len(fields) > 1 && strings.EqualFold(fields[1], "LR") {
				d.direction = flowRight
			}
			header = true
			continue
		}

		if !d.parseLine(line) {
			return nil
		}
	}

	if !header || len(d.nodes) == 0 {
		return nil
	}
	return d
}

// arrows are the edge operators, longest first so that "-.->" is not mistaken
// for "-." followed by something else.
var arrows = []struct {
	token  string
	dashed bool
}{
	{"-.->", true},
	{"==>", false},
	{"-->", false},
	{"---", false},
	{"-.-", true},
}

// parseLine reads one statement: a chain of nodes joined by arrows, or a bare
// node declaration.
func (d *diagram) parseLine(line string) bool {
	rest := line
	var prev *diagramNode

	for {
		// Find the next arrow, if any.
		arrowAt, arrowLen, dashed := -1, 0, false
		for _, a := range arrows {
			if i := strings.Index(rest, a.token); i >= 0 && (arrowAt < 0 || i < arrowAt) {
				arrowAt, arrowLen, dashed = i, len(a.token), a.dashed
			}
		}

		if arrowAt < 0 {
			node := d.parseNodeRef(strings.TrimSpace(rest))
			if node == nil {
				return strings.TrimSpace(rest) == ""
			}
			if prev != nil {
				d.edges = append(d.edges, diagramEdge{from: prev, to: node})
			}
			return true
		}

		from := d.parseNodeRef(strings.TrimSpace(rest[:arrowAt]))
		if from == nil {
			from = prev
		}
		if from == nil {
			return false
		}

		rest = strings.TrimSpace(rest[arrowAt+arrowLen:])

		// An optional |label| sits between the arrow and the target.
		label := ""
		if strings.HasPrefix(rest, "|") {
			end := strings.Index(rest[1:], "|")
			if end < 0 {
				return false
			}
			label = strings.TrimSpace(rest[1 : end+1])
			rest = strings.TrimSpace(rest[end+2:])
		}

		// The target ends where the next arrow begins.
		next := len(rest)
		for _, a := range arrows {
			if i := strings.Index(rest, a.token); i >= 0 && i < next {
				next = i
			}
		}
		to := d.parseNodeRef(strings.TrimSpace(rest[:next]))
		if to == nil {
			return false
		}

		d.edges = append(d.edges, diagramEdge{from: from, to: to, label: label, dashed: dashed})
		prev = to
		rest = rest[next:]
		if strings.TrimSpace(rest) == "" {
			return true
		}
	}
}

// parseNodeRef reads "A", "A[Label]", "A(Label)", "A{Label}" or "A((Label))",
// creating the node on first mention.
func (d *diagram) parseNodeRef(text string) *diagramNode {
	if text == "" {
		return nil
	}

	id := text
	label := ""
	shape := shapeRect
	found := false

	for _, delim := range []struct {
		open, close string
		shape       nodeShape
	}{
		{"((", "))", shapeCircle},
		{"[", "]", shapeRect},
		{"(", ")", shapeRound},
		{"{", "}", shapeDiamond},
	} {
		open := strings.Index(text, delim.open)
		if open <= 0 || !strings.HasSuffix(text, delim.close) {
			continue
		}
		id = strings.TrimSpace(text[:open])
		label = strings.TrimSpace(text[open+len(delim.open) : len(text)-len(delim.close)])
		label = strings.Trim(label, `"`)
		shape = delim.shape
		found = true
		break
	}

	if !validNodeID(id) {
		return nil
	}

	if node, ok := d.byID[id]; ok {
		// A later mention with a label names a node introduced bare earlier.
		if found && node.label == id {
			node.label, node.shape = label, shape
		}
		return node
	}

	if label == "" {
		label = id
	}
	node := &diagramNode{id: id, label: label, shape: shape}
	d.byID[id] = node
	d.nodes = append(d.nodes, node)
	return node
}

func validNodeID(id string) bool {
	if id == "" {
		return false
	}
	for i := 0; i < len(id); i++ {
		if !isAlpha(id[i]) && !isDigit(id[i]) && id[i] != '_' && id[i] != '-' {
			return false
		}
	}
	return true
}

// layout assigns every node a layer, an order within that layer, and a position.
func (d *diagram) layout() (width, height float64) {
	d.assignLayers()
	d.orderWithinLayers()
	return d.position()
}

// assignLayers puts each node one layer below its deepest predecessor.
//
// Cycles are the complication. A flowchart routinely contains one -- a retry
// loop, a state machine returning to idle -- and a cycle has no consistent
// layering at all: every node in it wants to be below the others. Relaxing
// naively just pushes nodes deeper on every pass until the bound stops it,
// which spreads the graph across a dozen empty layers and leaves long edges
// pointing at nothing.
//
// So back edges are found first, by depth-first search: an edge into a node
// that is still on the current DFS stack closes a cycle. Those edges are
// excluded from layering and drawn afterwards, pointing back up the diagram,
// which is what a reader expects a loop to look like.
func (d *diagram) assignLayers() {
	back := d.backEdges()

	for pass := 0; pass < len(d.nodes); pass++ {
		changed := false
		for i, e := range d.edges {
			if back[i] || e.from == e.to {
				continue
			}
			if want := e.from.layer + 1; want > e.to.layer {
				e.to.layer = want
				changed = true
			}
		}
		if !changed {
			break
		}
	}
}

// backEdges marks the edges that close a cycle.
//
// Standard three-colour depth-first search: white unvisited, grey on the stack,
// black finished. An edge to a grey node is a back edge. Nodes are visited in
// declaration order so that the result depends on the source rather than on map
// iteration, which keeps the rendered SVG byte-identical between runs.
func (d *diagram) backEdges() []bool {
	const (
		white = 0
		grey  = 1
		black = 2
	)
	colour := make(map[*diagramNode]int, len(d.nodes))
	out := make(map[*diagramNode][]int, len(d.nodes))
	for i, e := range d.edges {
		out[e.from] = append(out[e.from], i)
	}

	back := make([]bool, len(d.edges))

	var visit func(n *diagramNode)
	visit = func(n *diagramNode) {
		colour[n] = grey
		for _, i := range out[n] {
			target := d.edges[i].to
			switch colour[target] {
			case grey:
				back[i] = true
			case white:
				visit(target)
			}
		}
		colour[n] = black
	}

	for _, n := range d.nodes {
		if colour[n] == white {
			visit(n)
		}
	}
	return back
}

// layers groups nodes by layer, preserving declaration order within each.
func (d *diagram) layers() [][]*diagramNode {
	depth := 0
	for _, n := range d.nodes {
		if n.layer > depth {
			depth = n.layer
		}
	}
	out := make([][]*diagramNode, depth+1)
	for _, n := range d.nodes {
		n.order = len(out[n.layer])
		out[n.layer] = append(out[n.layer], n)
	}
	return out
}

// orderWithinLayers reduces edge crossings by the barycentre heuristic: put each
// node near the average position of its predecessors.
//
// Two downward passes. The full algorithm alternates directions until the
// crossing count stops improving; two passes get most of the benefit on the size
// of graph anyone puts in documentation, and the README says so.
func (d *diagram) orderWithinLayers() {
	incoming := map[*diagramNode][]*diagramNode{}
	for _, e := range d.edges {
		if e.from != e.to {
			incoming[e.to] = append(incoming[e.to], e.from)
		}
	}

	for pass := 0; pass < 2; pass++ {
		layers := d.layers()
		for _, layer := range layers[1:] {
			// A stable insertion sort by barycentre: nodes with no
			// predecessors keep their declaration order rather than drifting.
			scores := make(map[*diagramNode]float64, len(layer))
			for _, n := range layer {
				preds := incoming[n]
				if len(preds) == 0 {
					scores[n] = float64(n.order)
					continue
				}
				total := 0.0
				for _, p := range preds {
					total += float64(p.order)
				}
				scores[n] = total / float64(len(preds))
			}
			for i := 1; i < len(layer); i++ {
				for j := i; j > 0 && scores[layer[j]] < scores[layer[j-1]]; j-- {
					layer[j], layer[j-1] = layer[j-1], layer[j]
				}
			}
			for i, n := range layer {
				n.order = i
			}
		}
	}
}

// position sizes every node and lays the layers out, returning the canvas size.
func (d *diagram) position() (width, height float64) {
	for _, n := range d.nodes {
		text := fontRegular.measure(toASCII(n.label), diagramFontSize)
		n.w = max(text+2*diagramPadX, diagramMinWidth)
		n.h = diagramNodeHeight
		switch n.shape {
		case shapeDiamond:
			// A diamond's usable width is half its bounding box, so it needs a
			// wider box to hold the same label.
			n.w = max(text*1.6+diagramPadX, diagramMinWidth*1.4)
			n.h = diagramNodeHeight * 1.5
		case shapeCircle:
			side := max(text+diagramPadX*1.6, diagramNodeHeight*1.4)
			n.w, n.h = side, side
		}
	}

	layers := d.layers()

	// Along-axis extent of each layer, and the widest.
	extents := make([]float64, len(layers))
	widest := 0.0
	for i, layer := range layers {
		for j, n := range layer {
			if j > 0 {
				extents[i] += diagramNodeGap
			}
			extents[i] += d.along(n)
		}
		widest = max(widest, extents[i])
	}

	// Across-axis extent: the tallest node in each layer, plus the gaps.
	across := diagramMargin
	for i, layer := range layers {
		tallest := 0.0
		for _, n := range layer {
			tallest = max(tallest, d.across(n))
		}
		pos := diagramMargin + (widest-extents[i])/2
		for _, n := range layer {
			d.place(n, pos, across+(tallest-d.across(n))/2)
			pos += d.along(n) + diagramNodeGap
		}
		across += tallest
		if i < len(layers)-1 {
			across += diagramLayerGap
		}
	}

	if d.direction == flowRight {
		return across + diagramMargin, widest + 2*diagramMargin
	}
	return widest + 2*diagramMargin, across + diagramMargin
}

// along returns the node's size on the within-layer axis, and across on the
// layer axis. Everything above is written once and works for both directions.
func (d *diagram) along(n *diagramNode) float64 {
	if d.direction == flowRight {
		return n.h
	}
	return n.w
}

func (d *diagram) across(n *diagramNode) float64 {
	if d.direction == flowRight {
		return n.w
	}
	return n.h
}

func (d *diagram) place(n *diagramNode, along, across float64) {
	if d.direction == flowRight {
		n.x, n.y = across, along
		return
	}
	n.x, n.y = along, across
}

// anchors returns the points an edge leaves and enters at.
func (d *diagram) anchors(e diagramEdge) (x1, y1, x2, y2 float64) {
	from, to := e.from, e.to
	if d.direction == flowRight {
		return from.x + from.w, from.y + from.h/2, to.x, to.y + to.h/2
	}
	return from.x + from.w/2, from.y + from.h, to.x + to.w/2, to.y
}

// laneGap is the spacing between the routing lanes used by long edges.
const laneGap = 16.0

// routedEdge pairs an edge with the lane it is routed through. Lane zero means
// the edge is drawn straight, between adjacent layers.
type routedEdge struct {
	edge diagramEdge
	lane int
}

// routeEdges decides how each edge is drawn.
//
// An edge between adjacent layers is a straight line. Anything else -- an edge
// skipping a layer, or a back edge closing a loop -- is routed out to a lane
// beside the graph and back in, because drawn straight it would pass through
// whatever nodes lie between its endpoints. Real layout engines solve this by
// inserting dummy nodes and routing the edge through them; lanes are the
// simpler answer and they make a loop look like a loop.
func (d *diagram) routeEdges() ([]routedEdge, float64) {
	routed := make([]routedEdge, 0, len(d.edges))
	lanes := 0
	for _, e := range d.edges {
		if e.from == e.to {
			continue
		}
		lane := 0
		if e.to.layer-e.from.layer != 1 {
			lanes++
			lane = lanes
		}
		routed = append(routed, routedEdge{edge: e, lane: lane})
	}
	if lanes == 0 {
		return routed, 0
	}
	return routed, float64(lanes)*laneGap + laneGap
}

// SVG renders the diagram.
//
// Colours are left to CSS classes rather than written into the markup, so that
// a diagram follows the page into dark mode. The one exception is the arrowhead
// marker, which needs a concrete fill; it uses currentColor, and the class on
// the svg element sets that.
func (d *diagram) SVG() string {
	width, height := d.layout()
	routed, extra := d.routeEdges()
	if d.direction == flowRight {
		height += extra
	} else {
		width += extra
	}

	var sb strings.Builder
	fmt.Fprintf(&sb,
		`<svg class="bd-diagram" viewBox="0 0 %.0f %.0f" width="%.0f" height="%.0f" `+
			`xmlns="http://www.w3.org/2000/svg" role="img">`,
		width, height, width, height)

	// A text alternative, because a diagram that only exists visually excludes
	// anyone using a screen reader.
	fmt.Fprintf(&sb, "<title>%s</title>", escapeHTML(d.description()))

	sb.WriteString(`<defs><marker id="bd-arrow" viewBox="0 0 10 10" refX="9" refY="5" ` +
		`markerWidth="7" markerHeight="7" orient="auto-start-reverse">` +
		`<path d="M 0 0 L 10 5 L 0 10 z" fill="currentColor"/></marker></defs>`)

	// Edges first, so that nodes paint over the line ends.
	sb.WriteString(`<g class="bd-edges">`)
	for _, r := range routed {
		d.writeEdge(&sb, r, width, height)
	}
	sb.WriteString(`</g><g class="bd-nodes">`)
	for _, n := range d.nodes {
		d.writeNode(&sb, n)
	}
	sb.WriteString(`</g></svg>`)
	return sb.String()
}

func (d *diagram) writeEdge(sb *strings.Builder, r routedEdge, width, height float64) {
	e := r.edge
	class := "bd-edge"
	if e.dashed {
		class += " bd-edge-dashed"
	}

	var labelX, labelY float64

	if r.lane == 0 {
		x1, y1, x2, y2 := d.anchors(e)
		fmt.Fprintf(sb, `<line class="%s" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" `+
			`marker-end="url(#bd-arrow)"/>`, class, x1, y1, x2, y2)
		labelX, labelY = (x1+x2)/2, (y1+y2)/2
	} else {
		var path string
		path, labelX, labelY = d.lanePath(e, r.lane, width, height)
		fmt.Fprintf(sb, `<path class="%s" d="%s" marker-end="url(#bd-arrow)"/>`, class, path)
	}

	if e.label == "" {
		return
	}
	label := toASCII(e.label)
	w := fontRegular.measure(label, diagramFontSize-2) + 10
	fmt.Fprintf(sb,
		`<rect class="bd-edge-label-bg" x="%.1f" y="%.1f" width="%.1f" height="16" rx="3"/>`+
			`<text class="bd-edge-label" x="%.1f" y="%.1f" text-anchor="middle">%s</text>`,
		labelX-w/2, labelY-8, w, labelX, labelY+4, escapeHTML(label))
}

// lanePath routes an edge out to the side of the graph and back, returning the
// path and a point to put the label at.
func (d *diagram) lanePath(e diagramEdge, lane int, width, height float64) (path string, labelX, labelY float64) {
	from, to := e.from, e.to
	offset := float64(lane) * laneGap

	if d.direction == flowRight {
		ly := height - offset
		x1, y1 := from.x+from.w/2, from.y+from.h
		x2, y2 := to.x+to.w/2, to.y+to.h
		return fmt.Sprintf("M %.1f %.1f V %.1f H %.1f V %.1f", x1, y1, ly, x2, y2), (x1 + x2) / 2, ly
	}

	lx := width - offset
	x1, y1 := from.x+from.w, from.y+from.h/2
	x2, y2 := to.x+to.w, to.y+to.h/2
	return fmt.Sprintf("M %.1f %.1f H %.1f V %.1f H %.1f", x1, y1, lx, y2, x2), lx, (y1 + y2) / 2
}

func (d *diagram) writeNode(sb *strings.Builder, n *diagramNode) {
	switch n.shape {
	case shapeDiamond:
		fmt.Fprintf(sb, `<polygon class="bd-node" points="%.1f,%.1f %.1f,%.1f %.1f,%.1f %.1f,%.1f"/>`,
			n.x+n.w/2, n.y, n.x+n.w, n.y+n.h/2, n.x+n.w/2, n.y+n.h, n.x, n.y+n.h/2)
	case shapeCircle:
		fmt.Fprintf(sb, `<ellipse class="bd-node" cx="%.1f" cy="%.1f" rx="%.1f" ry="%.1f"/>`,
			n.x+n.w/2, n.y+n.h/2, n.w/2, n.h/2)
	default:
		radius := 4.0
		if n.shape == shapeRound {
			radius = n.h / 2
		}
		fmt.Fprintf(sb, `<rect class="bd-node" x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f"/>`,
			n.x, n.y, n.w, n.h, radius)
	}

	fmt.Fprintf(sb, `<text class="bd-node-text" x="%.1f" y="%.1f" text-anchor="middle">%s</text>`,
		n.x+n.w/2, n.y+n.h/2+4.5, escapeHTML(toASCII(n.label)))
}

// description is the diagram in words, for the SVG title element.
func (d *diagram) description() string {
	var sb strings.Builder
	sb.WriteString("Flowchart: ")
	for i, e := range d.edges {
		if i > 0 {
			sb.WriteString("; ")
		}
		if i == 6 {
			fmt.Fprintf(&sb, "and %d more", len(d.edges)-6)
			break
		}
		sb.WriteString(e.from.label)
		if e.label != "" {
			sb.WriteString(" (" + e.label + ")")
		}
		sb.WriteString(" to ")
		sb.WriteString(e.to.label)
	}
	if len(d.edges) == 0 {
		fmt.Fprintf(&sb, "%d nodes, no connections", len(d.nodes))
	}
	return sb.String()
}

// renderDiagram turns fence content into SVG, reporting whether it was a
// diagram at all. A fence that fails to parse falls back to being shown as
// code, which is more useful than an error message where a picture should be.
func renderDiagram(src string) (string, bool) {
	d := parseDiagram(src)
	if d == nil {
		return "", false
	}
	return d.SVG(), true
}
