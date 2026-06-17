package Services

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// angularBooleanAttrs are Angular directive attributes used without a value.
// Bluemonday normalizes them to attr="", so we strip the empty value afterward.
var angularBooleanAttrs = []string{"app-ulinks", "appRouterLinks", "app-navlinks"}

// angularAttrCasefix restores Angular attribute casing that bluemonday lowercases.
var angularAttrCasefix = map[string]string{
	"[innerhtml]": "[innerHTML]",
}

// angularCustomElements are Angular component tags that the HTML parser discards
// as unknown elements. We preserve them via pre/post processing.
var angularCustomElements = []string{"app-notifications"}

var angularElementRegexps = func() map[string]*regexp.Regexp {
	m := make(map[string]*regexp.Regexp, len(angularCustomElements))
	for _, el := range angularCustomElements {
		m[el] = regexp.MustCompile(`(?i)<` + regexp.QuoteMeta(el) + `(\s[^>]*)?>[\s\S]*?</` + regexp.QuoteMeta(el) + `>`)
	}
	return m
}()

func preserveAngularElements(input string) (string, []string) {
	var saved []string
	for _, el := range angularCustomElements {
		re := angularElementRegexps[el]
		input = re.ReplaceAllStringFunc(input, func(match string) string {
			idx := len(saved)
			saved = append(saved, match)
			return fmt.Sprintf(`<span data-angular-placeholder="%d"></span>`, idx)
		})
	}
	return input, saved
}

func restoreAngularElements(input string, saved []string) string {
	for i, original := range saved {
		placeholder := fmt.Sprintf(`<span data-angular-placeholder="%d"></span>`, i)
		input = strings.ReplaceAll(input, placeholder, original)
	}
	return input
}

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
	p.AllowAttrs("class", "id", "style", "data-angular-placeholder").Globally()

	// Angular structural/directive attributes
	p.AllowAttrs("app-ulinks", "appRouterLinks", "app-navlinks", "[innerHTML]", "i18n").Globally()

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
	html, saved := preserveAngularElements(html)
	result := templatePolicy.Sanitize(html)
	result = restoreAngularElements(result, saved)
	for _, attr := range angularBooleanAttrs {
		result = strings.ReplaceAll(result, attr+`=""`, attr)
	}
	for lower, correct := range angularAttrCasefix {
		result = strings.ReplaceAll(result, lower, correct)
	}
	return result
}
