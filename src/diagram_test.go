package main

import (
	"strings"
	"testing"
)

func TestParseDiagram(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		nodes     int
		edges     int
		direction diagramDirection
	}{
		{"simple chain", "graph TD\nA --> B", 2, 1, flowDown},
		{"left to right", "graph LR\nA --> B", 2, 1, flowRight},
		{"flowchart keyword", "flowchart TD\nA --> B", 2, 1, flowDown},
		{"chained", "graph TD\nA --> B --> C", 3, 2, flowDown},
		{"labels", "graph TD\nA[Start] --> B[End]", 2, 1, flowDown},
		{"edge label", "graph TD\nA -->|yes| B", 2, 1, flowDown},
		{"reused id", "graph TD\nA[Start] --> B\nA --> C", 3, 2, flowDown},
		{"dashed", "graph TD\nA -.-> B", 2, 1, flowDown},
		{"comment ignored", "graph TD\n%% note\nA --> B", 2, 1, flowDown},
		{"blank lines", "graph TD\n\nA --> B\n\n", 2, 1, flowDown},
		{"cycle", "graph TD\nA --> B\nB --> A", 2, 2, flowDown},
		{"bare node", "graph TD\nA", 1, 0, flowDown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := parseDiagram(tt.in)
			if d == nil {
				t.Fatalf("parseDiagram(%q) returned nil", tt.in)
			}
			if len(d.nodes) != tt.nodes {
				t.Errorf("got %d nodes, want %d", len(d.nodes), tt.nodes)
			}
			if len(d.edges) != tt.edges {
				t.Errorf("got %d edges, want %d", len(d.edges), tt.edges)
			}
			if d.direction != tt.direction {
				t.Errorf("got direction %v, want %v", d.direction, tt.direction)
			}
		})
	}
}

func TestParseDiagramShapes(t *testing.T) {
	d := parseDiagram("graph TD\nA[rect] --> B(round)\nB --> C{diamond}\nC --> D((circle))")
	if d == nil {
		t.Fatal("nil diagram")
	}
	want := []struct {
		id    string
		label string
		shape nodeShape
	}{
		{"A", "rect", shapeRect},
		{"B", "round", shapeRound},
		{"C", "diamond", shapeDiamond},
		{"D", "circle", shapeCircle},
	}
	for i, w := range want {
		got := d.nodes[i]
		if got.id != w.id || got.label != w.label || got.shape != w.shape {
			t.Errorf("node %d = {%s %q %v}, want {%s %q %v}",
				i, got.id, got.label, got.shape, w.id, w.label, w.shape)
		}
	}
}

// TestParseDiagramRejectsNonDiagrams matters because a fence that is not a
// diagram must fall back to being rendered as code, not produce an error where
// a picture should be.
func TestParseDiagramRejectsNonDiagrams(t *testing.T) {
	for _, src := range []string{
		"", "func main() {}", "SELECT * FROM t", "# heading",
		"graph", "gantt\ntitle x", "A --> B",
	} {
		t.Run(src, func(t *testing.T) {
			if d := parseDiagram(src); d != nil && len(d.edges) > 0 {
				t.Errorf("parseDiagram(%q) produced a diagram; it should decline", src)
			}
		})
	}
}

// TestDiagramCycleLayering is the regression test for the bug this feature
// shipped with first: relaxing layers over a cycle pushed nodes deeper on every
// pass, spreading the graph across empty layers.
func TestDiagramCycleLayering(t *testing.T) {
	d := parseDiagram("graph TD\nA --> B\nB --> C\nC --> D\nD --> B")
	if d == nil {
		t.Fatal("nil diagram")
	}
	d.layout()

	layers := map[string]int{}
	for _, n := range d.nodes {
		layers[n.id] = n.layer
	}
	// A=0, B=1, C=2, D=3. The back edge D->B must not push B down.
	for id, want := range map[string]int{"A": 0, "B": 1, "C": 2, "D": 3} {
		if layers[id] != want {
			t.Errorf("node %s is on layer %d, want %d (layers: %v)", id, layers[id], want, layers)
		}
	}

	back := d.backEdges()
	if !back[3] {
		t.Error("D --> B was not identified as a back edge")
	}
	if back[0] || back[1] || back[2] {
		t.Error("a forward edge was misidentified as a back edge")
	}
}

// TestDiagramLongEdgesAreRouted checks that an edge spanning more than one
// layer gets a lane rather than being drawn straight through the nodes between
// its endpoints.
func TestDiagramLongEdgesAreRouted(t *testing.T) {
	d := parseDiagram("graph TD\nA --> B\nB --> C\nA --> C")
	if d == nil {
		t.Fatal("nil diagram")
	}
	d.layout()
	routed, extra := d.routeEdges()

	if extra <= 0 {
		t.Error("a layer-skipping edge did not widen the canvas for its lane")
	}
	lanes := 0
	for _, r := range routed {
		if r.lane > 0 {
			lanes++
		}
	}
	if lanes != 1 {
		t.Errorf("%d edges were routed through lanes, want exactly 1 (A --> C)", lanes)
	}
}

func TestDiagramSVG(t *testing.T) {
	svg, ok := renderDiagram("graph TD\nA[Start] -->|go| B{Choose}\nB --> C((End))")
	if !ok {
		t.Fatal("renderDiagram declined a valid diagram")
	}
	for _, want := range []string{
		"<svg", "</svg>", "viewBox=", "<title>", "bd-node", "bd-edge",
		"marker-end", "<polygon", "<ellipse", "<rect", "Start", "Choose", "End", "go",
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("SVG is missing %q", want)
		}
	}
	// The title is the accessible description; a diagram that exists only
	// visually excludes anyone using a screen reader.
	if !strings.Contains(svg, "Flowchart:") {
		t.Error("SVG has no accessible description")
	}
}

func TestDiagramSVGEscaping(t *testing.T) {
	svg, ok := renderDiagram(`graph TD
A[<script>alert(1)</script>] --> B["quote & amp"]`)
	if !ok {
		t.Fatal("renderDiagram declined")
	}
	if strings.Contains(svg, "<script>") {
		t.Error("a label escaped into markup")
	}
	for _, want := range []string{"&lt;script&gt;", "&amp;"} {
		if !strings.Contains(svg, want) {
			t.Errorf("expected %q in the escaped output", want)
		}
	}
}

// TestDiagramIsDeterministic guards the reproducible build. Layout consults
// several maps, and Go randomises map iteration.
func TestDiagramIsDeterministic(t *testing.T) {
	const src = `graph TD
A[Start] --> B{Check}
B -->|yes| C[Work]
B -->|no| E((Wait))
C --> D[Notify]
D --> E
E --> B`
	first, ok := renderDiagram(src)
	if !ok {
		t.Fatal("renderDiagram declined")
	}
	for i := 0; i < 8; i++ {
		next, _ := renderDiagram(src)
		if next != first {
			t.Fatalf("SVG differs between renders on attempt %d", i+2)
		}
	}
}

// TestDiagramFallsBackToCode checks the renderer's behaviour, not the parser's:
// a mermaid fence that does not parse must still show its content.
func TestDiagramFallsBackToCode(t *testing.T) {
	html := RenderHTMLWith(Parse("```mermaid\nnot a diagram at all\n```\n"),
		RenderOptions{Diagrams: true})
	if strings.Contains(html, "<svg") {
		t.Error("unparseable content produced an SVG")
	}
	if !strings.Contains(html, "not a diagram at all") {
		t.Error("content was lost instead of falling back to a code block")
	}
}

// TestDiagramsAreOptional keeps the conformance number honest: the spec runner
// renders with the zero value, and a mermaid fence is a code block there.
func TestDiagramsAreOptional(t *testing.T) {
	const src = "```mermaid\ngraph TD\nA --> B\n```\n"
	if strings.Contains(RenderHTML(Parse(src)), "<svg") {
		t.Error("diagrams rendered without the option being set")
	}
	if !strings.Contains(RenderHTMLWith(Parse(src), RenderOptions{Diagrams: true}), "<svg") {
		t.Error("diagrams did not render with the option set")
	}
}

func FuzzDiagram(f *testing.F) {
	for _, seed := range []string{
		"graph TD\nA --> B", "graph LR\nA[x] -->|l| B{y}", "graph TD\nA --> A",
		"graph TD\n" + strings.Repeat("A --> B\n", 50), "graph", "flowchart TD\nA((x))",
		"graph TD\nA --> B --> C --> A", "graph TD\nA[", "graph TD\n|",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		svg, ok := renderDiagram(src)
		if !ok {
			return
		}
		if !strings.HasPrefix(svg, "<svg") || !strings.HasSuffix(svg, "</svg>") {
			t.Fatalf("malformed SVG for input %q", src)
		}
		if strings.Count(svg, "<svg") != 1 {
			t.Fatalf("nested svg elements for input %q", src)
		}
	})
}
