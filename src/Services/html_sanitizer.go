package Services

import "github.com/microcosm-cc/bluemonday"

// templatePolicy allows rich HTML for component templates but strips
// all JavaScript vectors: <script>, inline event handlers, <iframe>,
// javascript: URLs, data: URIs, and similar.
var templatePolicy = func() *bluemonday.Policy {
	p := bluemonday.NewPolicy()

	p.AllowStandardURLs()

	p.AllowElements(
		"acronym", "address", "article", "aside",
		"b", "blockquote", "br",
		"caption", "cite", "col", "colgroup",
		"dd", "del", "details", "dfn", "div", "dl", "dt",
		"em",
		"figcaption", "figure", "footer",
		"h1", "h2", "h3", "h4", "h5", "h6", "header", "hr",
		"i", "ins",
		"kbd",
		"li",
		"main", "mark",
		"nav",
		"ol",
		"q",
		"s", "samp", "section", "small", "source", "span", "strong", "sub", "summary", "sup",
		"u", "ul",
		"var",
		"wbr",
	)

	p.AllowAttrs("cite").OnElements("blockquote", "q", "del", "ins")
	p.AllowAttrs("datetime").OnElements("time", "del", "ins")
	p.AllowAttrs("open").OnElements("details")
	p.AllowAttrs("span", "width").OnElements("col", "colgroup")
	p.AllowAttrs("colspan", "rowspan", "headers", "scope").OnElements("td", "th")
	p.AllowAttrs("class", "id", "style").Globally()

	p.AllowStyles("color", "background-color", "background",
		"font-size", "font-weight", "font-style", "font-family",
		"text-align", "text-decoration",
		"margin", "margin-top", "margin-right", "margin-bottom", "margin-left",
		"padding", "padding-top", "padding-right", "padding-bottom", "padding-left",
		"border", "border-radius",
		"width", "height", "max-width", "max-height",
		"display", "flex-direction", "align-items", "justify-content", "gap",
		"list-style", "list-style-type",
	).Globally()

	return p
}()

// SanitizeTemplate strips all JavaScript vectors from an HTML template
// while preserving structural and presentational markup.
func SanitizeTemplate(html string) string {
	return templatePolicy.Sanitize(html)
}
