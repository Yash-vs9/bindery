package main

import (
	"html/template"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// The theme.
//
// The page shell, its stylesheet and its client script are Go string constants
// rather than files loaded at runtime, so that the binary is genuinely
// standalone: nothing to install, nothing to find on disk, and one artifact to
// hash for the reproducible-build claim.
//
// Note for anyone editing pageScript: Go raw string literals are delimited by
// backticks, so the JavaScript in this file cannot use template literals.
// Ordinary quotes and concatenation only.

// pageTemplate is parsed once, on first use. sync.OnceValue replaces the
// once_cell-style lazy initialisation this would otherwise need, and keeps the
// parse off the init path so that "bindery version" does no work it need not.
var pageTemplate = sync.OnceValue(func() *template.Template {
	return template.Must(template.New("page").Parse(pageHTML))
})

// renderPage wraps a rendered body in the site shell.
//
// html/template does the escaping. It is contextual: a value interpolated into
// an href is escaped as a URL, and one interpolated into element content is
// escaped as HTML. The already-rendered Markdown body is the one value marked
// safe, because the Markdown renderer escaped it on the way through.
func renderPage(s *Site, p *Page, live bool) (string, error) {
	var sb strings.Builder
	data := struct {
		Title    string
		SiteName string
		Body     template.HTML
		Nav      []NavEntry
		TOC      []Heading
		Live     bool
		Style    template.CSS
		Script   template.JS
		SearchJS template.JS
	}{
		Title:    p.Title,
		SiteName: "docs",
		Body:     template.HTML(p.Body),
		Nav:      s.Nav(p.URL),
		TOC:      TOC(p.Headings),
		Live:     live,
		Style:    template.CSS(pageCSS),
		Script:   template.JS(pageScript),
		SearchJS: template.JS(stopWordPrelude() + searchScript),
	}
	if err := pageTemplate().Execute(&sb, data); err != nil {
		return "", err
	}
	return sb.String(), nil
}

const pageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>{{.Style}}</style>
</head>
<body>
<div class="layout">
<nav class="sidebar">
<div class="brand">{{.SiteName}}</div>
<input id="bindery-search" type="search" placeholder="Search    /" autocomplete="off" spellcheck="false">
<div id="bindery-results" hidden></div>
<ul>
{{range .Nav}}<li><a href="{{.URL}}"{{if .Current}} class="current"{{end}}>{{.Title}}</a>
{{if .Current}}{{if $.TOC}}<ul class="toc">
{{range $.TOC}}<li class="toc-{{.Level}}"><a href="#{{.Slug}}">{{.Text}}</a></li>
{{end}}</ul>
{{end}}{{end}}</li>
{{end}}</ul>
</nav>
<main>
<article>
{{.Body}}
</article>
</main>
</div>
<script>{{.SearchJS}}</script>
{{if .Live}}<script>{{.Script}}</script>{{end}}
</body>
</html>
`

const pageCSS = `
:root {
  --bg: #ffffff;
  --fg: #1c1c1e;
  --muted: #6b6b70;
  --rule: #e3e3e6;
  --accent: #2f5fd0;
  --code-bg: #f5f5f7;
  --sidebar-bg: #fafafa;
  --hl-kw: #8f3fbf;
  --hl-str: #197a3d;
  --hl-num: #b3541e;
  --hl-com: #7a7a80;
  --hl-typ: #1b6f8c;
  --hl-fn: #2f5fd0;
  --hl-ins: #197a3d;
  --hl-del: #b3261e;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #16171a;
    --fg: #e6e6e8;
    --muted: #9a9aa2;
    --rule: #2c2d31;
    --accent: #7aa2f7;
    --code-bg: #1e1f24;
    --sidebar-bg: #121316;
    --hl-kw: #c792ea;
    --hl-str: #8bd49c;
    --hl-num: #f78c6c;
    --hl-com: #6b6f7a;
    --hl-typ: #7fd1e0;
    --hl-fn: #82aaff;
    --hl-ins: #8bd49c;
    --hl-del: #ff8a80;
  }
}
* { box-sizing: border-box; }
html { -webkit-text-size-adjust: 100%; }
body {
  margin: 0;
  background: var(--bg);
  color: var(--fg);
  font: 16px/1.65 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
}
.layout { display: flex; min-height: 100vh; }
.sidebar {
  width: 16rem;
  flex: 0 0 16rem;
  padding: 1.75rem 1.25rem;
  background: var(--sidebar-bg);
  border-right: 1px solid var(--rule);
}
.brand {
  font-weight: 600;
  letter-spacing: .01em;
  margin-bottom: 1rem;
  color: var(--muted);
  text-transform: lowercase;
}
.sidebar ul { list-style: none; margin: 0; padding: 0; }
.sidebar li { margin: .15rem 0; }
.sidebar a {
  display: block;
  padding: .3rem .5rem;
  border-radius: 5px;
  color: var(--fg);
  text-decoration: none;
  font-size: .93rem;
}
.sidebar a:hover { background: var(--code-bg); }
.sidebar a.current { color: var(--accent); font-weight: 600; }
#bindery-search {
  width: 100%;
  margin-bottom: .9rem;
  padding: .4rem .55rem;
  font: inherit;
  font-size: .88rem;
  color: var(--fg);
  background: var(--bg);
  border: 1px solid var(--rule);
  border-radius: 6px;
}
#bindery-search:focus { outline: 2px solid var(--accent); outline-offset: -1px; }
#bindery-results {
  margin: -.5rem 0 .9rem;
  border: 1px solid var(--rule);
  border-radius: 6px;
  background: var(--bg);
  overflow: hidden;
}
#bindery-results .hit {
  display: block;
  padding: .45rem .6rem;
  border-bottom: 1px solid var(--rule);
  color: var(--fg);
  text-decoration: none;
}
#bindery-results .hit:last-child { border-bottom: 0; }
#bindery-results .hit:hover, #bindery-results .hit.active { background: var(--code-bg); }
#bindery-results .hit-title { font-size: .87rem; font-weight: 600; }
#bindery-results .hit-crumb { color: var(--muted); font-weight: 400; }
#bindery-results .hit-preview {
  font-size: .8rem;
  color: var(--muted);
  margin-top: .12rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
#bindery-results .empty { padding: .5rem .6rem; font-size: .84rem; color: var(--muted); }
.toc { margin: .1rem 0 .5rem; padding-left: .55rem; border-left: 1px solid var(--rule); }
.toc a { font-size: .87rem; color: var(--muted); padding: .18rem .5rem; }
.toc a:hover { color: var(--fg); }
.toc-3 a { padding-left: 1.1rem; font-size: .84rem; }
h2, h3 { scroll-margin-top: 1rem; }
main { flex: 1; min-width: 0; display: flex; justify-content: center; }
article { width: 100%; max-width: 44rem; padding: 2.5rem 2rem 6rem; }
article > :first-child { margin-top: 0; }
h1, h2, h3, h4, h5, h6 { line-height: 1.25; margin: 2rem 0 .75rem; }
h1 { font-size: 2rem; letter-spacing: -.02em; }
h2 { font-size: 1.45rem; padding-bottom: .3rem; border-bottom: 1px solid var(--rule); }
h3 { font-size: 1.15rem; }
a { color: var(--accent); }
p, ul, ol, blockquote { margin: 0 0 1rem; }
code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: .88em;
  background: var(--code-bg);
  padding: .15em .35em;
  border-radius: 4px;
}
pre {
  background: var(--code-bg);
  padding: .9rem 1.1rem;
  border-radius: 8px;
  overflow-x: auto;
  margin: 0 0 1.25rem;
}
pre code { background: none; padding: 0; font-size: .85rem; line-height: 1.55; }
.hl-kw  { color: var(--hl-kw); }
.hl-str { color: var(--hl-str); }
.hl-num { color: var(--hl-num); }
.hl-com { color: var(--hl-com); font-style: italic; }
.hl-typ { color: var(--hl-typ); }
.hl-fn  { color: var(--hl-fn); }
.hl-ins { color: var(--hl-ins); }
.hl-del { color: var(--hl-del); }
blockquote {
  border-left: 3px solid var(--rule);
  padding-left: 1rem;
  color: var(--muted);
}
hr { border: 0; border-top: 1px solid var(--rule); margin: 2rem 0; }
img { max-width: 100%; }
@media (max-width: 720px) {
  .layout { display: block; }
  .sidebar { width: auto; flex: none; border-right: 0; border-bottom: 1px solid var(--rule); }
  article { padding: 1.5rem 1.1rem 4rem; }
}
`

// pageScript is the live-reload client. It is injected only by "bindery dev",
// never by "bindery build", so a published site carries no script at all.
const pageScript = `
(function () {
  var reconnectDelay = 250;
  function connect() {
    var proto = location.protocol === "https:" ? "wss://" : "ws://";
    var socket = new WebSocket(proto + location.host + "/__bindery/live");
    socket.onmessage = function (event) {
      if (event.data === "reload") location.reload();
    };
    socket.onopen = function () { reconnectDelay = 250; };
    socket.onclose = function () {
      // The server going away is the normal case during a restart, so back off
      // and keep trying rather than leaving a dead page.
      setTimeout(connect, reconnectDelay);
      reconnectDelay = Math.min(reconnectDelay * 2, 4000);
    };
  }
  connect();
})();
`

// stopWordPrelude emits the stop-word list as JavaScript.
//
// The list is generated from the Go map rather than written out twice, so that
// half of the tokeniser rule has exactly one source of truth. Two hand-kept
// copies would drift, and the symptom of drift is a query that silently returns
// nothing.
//
// The output is sorted so that two builds of the same source produce identical
// bytes, which the reproducible-build claim depends on.
func stopWordPrelude() string {
	words := make([]string, 0, len(stopWords))
	for word := range stopWords {
		words = append(words, word)
	}
	sort.Strings(words)

	var sb strings.Builder
	sb.WriteString("var BINDERY_STOP = {")
	for i, word := range words {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(strconv.Quote(word))
		sb.WriteString(":1")
	}
	sb.WriteString("};\n")
	return sb.String()
}

// searchScript is the client half of search: it fetches the index, ranks with
// BM25, and renders results. No library, and no framework.
//
// The tokeniser here must agree exactly with tokenise() in search.go:
//
//	lowercase, split on non-letter/non-digit, drop tokens shorter than two
//	characters, drop stop words
//
// Fixtures both sides agree on, from TestTokeniseFixtures:
//
//	"Hello, World!"        -> ["hello","world"]
//	"snake_case_name"      -> ["snake","case","name"]
//	"v1.27 and Go1.27"     -> ["v1","27","go1","27"]
//	"CJK 日本語テキスト"     -> ["cjk","日本語テキスト"]
//	"emoji 🎉 party"        -> ["emoji","party"]
//	"C++ and C#"           -> []
//	"a is the of"          -> []
//
// Verified: both implementations were run against all thirteen fixtures in
// TestTokeniseFixtures and agreed on every one.
//
// Reminder: Go raw strings are backtick-delimited, so no template literals.
const searchScript = `
(function () {
  var input = document.getElementById("bindery-search");
  var panel = document.getElementById("bindery-results");
  if (!input || !panel) return;

  var index = null, pending = null, hits = [], active = -1;
  var K1 = 1.2, B = 0.75, MAX_HITS = 8;

  function tokenize(text) {
    return text.toLowerCase().split(/[^\p{L}\p{N}]+/u).filter(function (t) {
      return t.length >= 2 && !BINDERY_STOP[t];
    });
  }

  function load() {
    if (index || pending) return pending;
    pending = fetch("/search-index.json")
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (data) { index = data; return data; })
      .catch(function () { return null; });
    return pending;
  }

  // Postings for a term: an exact match, plus prefix matches so that typing
  // "pars" finds "parser" before the word is finished.
  function postingsFor(term, isLast) {
    var out = index.terms[term] ? index.terms[term].slice() : [];
    if (!isLast || term.length < 2) return out;
    var keys = Object.keys(index.terms);
    for (var i = 0; i < keys.length; i++) {
      if (keys[i] !== term && keys[i].indexOf(term) === 0) {
        out = out.concat(index.terms[keys[i]]);
      }
    }
    return out;
  }

  function search(query) {
    var terms = tokenize(query);
    if (!terms.length) return [];
    var total = index.docs.length, scores = {};

    terms.forEach(function (term, i) {
      var postings = postingsFor(term, i === terms.length - 1);
      var df = postings.length;
      if (!df) return;
      var idf = Math.log(1 + (total - df + 0.5) / (df + 0.5));
      postings.forEach(function (p) {
        var doc = index.docs[p[0]], tf = p[1];
        var norm = 1 - B + B * (doc.n / index.avg);
        scores[p[0]] = (scores[p[0]] || 0) + idf * tf * (K1 + 1) / (tf + K1 * norm);
      });
    });

    return Object.keys(scores)
      .map(function (id) { return { doc: index.docs[id], score: scores[id] }; })
      .sort(function (a, b) { return b.score - a.score; })
      .slice(0, MAX_HITS);
  }

  function render() {
    if (!hits.length) {
      panel.innerHTML = "<div class=\"empty\">No matches</div>";
      panel.hidden = false;
      return;
    }
    panel.innerHTML = "";
    hits.forEach(function (hit, i) {
      var a = document.createElement("a");
      a.className = "hit" + (i === active ? " active" : "");
      a.href = hit.doc.u;

      var title = document.createElement("div");
      title.className = "hit-title";
      title.textContent = hit.doc.t;
      if (hit.doc.h) {
        var crumb = document.createElement("span");
        crumb.className = "hit-crumb";
        crumb.textContent = " \u203a " + hit.doc.h;
        title.appendChild(crumb);
      }

      var preview = document.createElement("div");
      preview.className = "hit-preview";
      preview.textContent = hit.doc.p || "";

      a.appendChild(title);
      a.appendChild(preview);
      panel.appendChild(a);
    });
    panel.hidden = false;
  }

  function close() {
    panel.hidden = true;
    hits = [];
    active = -1;
  }

  function update() {
    var query = input.value.trim();
    if (!query) { close(); return; }
    load().then(function () {
      if (!index) { close(); return; }
      hits = search(query);
      active = -1;
      render();
    });
  }

  var timer = null;
  input.addEventListener("input", function () {
    clearTimeout(timer);
    timer = setTimeout(update, 90);
  });
  input.addEventListener("focus", load);

  input.addEventListener("keydown", function (e) {
    if (e.key === "Escape") { close(); input.blur(); return; }
    if (!hits.length) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      active = (active + 1) % hits.length;
      render();
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      active = (active - 1 + hits.length) % hits.length;
      render();
    } else if (e.key === "Enter" && active >= 0) {
      e.preventDefault();
      location.href = hits[active].doc.u;
    }
  });

  document.addEventListener("keydown", function (e) {
    if (e.key === "/" && document.activeElement !== input) {
      e.preventDefault();
      input.focus();
    }
  });

  document.addEventListener("click", function (e) {
    if (!panel.contains(e.target) && e.target !== input) close();
  });
})();
`
