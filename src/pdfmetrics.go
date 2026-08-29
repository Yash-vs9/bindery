package main

// Base-14 font metrics.
//
// A PDF that uses the fourteen standard fonts embeds no font programme: every
// conforming reader already has them. That is what makes a dependency-free PDF
// writer possible at all -- there is no font parsing, no hinting and no
// rasterisation here, only arithmetic.
//
// What a writer still needs is advance widths, in order to break lines. These
// tables hold them for printable ASCII, in 1/1000 em, taken from the published
// Adobe Font Metrics for the base-14 set. The provenance is recorded in
// STDLIB.md: they are measured data, not code, and they were generated from the
// AFM files rather than transcribed by hand.
//
// Courier is monospaced, so its table is a single number.

// asciiFirst is the code point the width tables start at.
const asciiFirst = 32

// helveticaWidths holds advance widths for Helvetica, code points 32 to 126.
var helveticaWidths = [95]int16{
	278, 278, 355, 556, 556, 889, 667, 222, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556,
	556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556, 1015, 667, 667, 722,
	722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778, 667, 778, 722, 667, 611, 722,
	667, 944, 667, 667, 611, 278, 278, 278, 469, 556, 222, 556, 556, 500, 556, 556, 278, 556,
	556, 222, 222, 500, 222, 833, 556, 556, 556, 556, 333, 500, 278, 556, 500, 722, 500, 500,
	500, 334, 260, 334, 584,
}

// helveticaBoldWidths holds advance widths for Helvetica-Bold.
var helveticaBoldWidths = [95]int16{
	278, 333, 474, 556, 556, 889, 722, 278, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556,
	556, 556, 556, 556, 556, 556, 556, 556, 333, 333, 584, 584, 584, 611, 975, 722, 722, 722,
	722, 667, 611, 778, 722, 278, 556, 722, 611, 833, 722, 778, 667, 778, 722, 667, 611, 722,
	667, 944, 667, 667, 611, 333, 278, 333, 584, 556, 278, 556, 611, 556, 611, 556, 333, 611,
	611, 278, 278, 556, 278, 889, 611, 611, 611, 611, 389, 556, 333, 611, 556, 778, 556, 556,
	500, 389, 280, 389, 584,
}

// helveticaObliqueWidths holds advance widths for Helvetica-Oblique.
var helveticaObliqueWidths = [95]int16{
	278, 278, 355, 556, 556, 889, 667, 222, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556,
	556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556, 1015, 667, 667, 722,
	722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778, 667, 778, 722, 667, 611, 722,
	667, 944, 667, 667, 611, 278, 278, 278, 469, 556, 222, 556, 556, 500, 556, 556, 278, 556,
	556, 222, 222, 500, 222, 833, 556, 556, 556, 556, 333, 500, 278, 556, 500, 722, 500, 500,
	500, 334, 260, 334, 584,
}

// courierWidth is the advance width of every Courier glyph.
const courierWidth = 600
