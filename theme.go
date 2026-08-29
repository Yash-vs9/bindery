package main

import (
	"html/template"
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
	}{
		Title:    p.Title,
		SiteName: "docs",
		Body:     template.HTML(p.Body),
		Nav:      s.Nav(p.URL),
		TOC:      TOC(p.Headings),
		Live:     live,
		Style:    template.CSS(pageCSS),
		Script:   template.JS(pageScript),
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
