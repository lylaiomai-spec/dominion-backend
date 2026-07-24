package Services

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ParseMybbHTML converts MyBB-generated HTML back to BB code.
// Scripts, iframes, and javascript:/data: URIs are stripped entirely.
func ParseMybbHTML(rawHTML string) string {
	ctx := &html.Node{
		Type:     html.ElementNode,
		Data:     "div",
		DataAtom: atom.Div,
	}
	nodes, err := html.ParseFragment(strings.NewReader(rawHTML), ctx)
	if err != nil {
		return rawHTML
	}
	var sb strings.Builder
	for _, n := range nodes {
		sb.WriteString(mybbNode(n))
	}
	return strings.TrimSpace(sb.String())
}

func mybbNode(n *html.Node) string {
	switch n.Type {
	case html.TextNode:
		return n.Data
	case html.ElementNode:
		return mybbElement(n)
	case html.DocumentNode:
		return mybbChildren(n)
	}
	return ""
}

func mybbChildren(n *html.Node) string {
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(mybbNode(c))
	}
	return sb.String()
}

func mybbElement(n *html.Node) string {
	switch n.Data {
	// Dangerous — drop entirely including children
	case "script", "iframe", "object", "embed", "form", "style", "meta", "link", "base":
		return ""

	case "br":
		return "\n"

	case "p":
		inner := mybbChildren(n)
		if strings.TrimSpace(inner) == "" {
			return "\n"
		}
		return inner + "\n"

	case "strong":
		if mybbHasClass(n, "legend") {
			return "" // code-box label ("Код:")
		}
		return "[b]" + mybbChildren(n) + "[/b]"

	case "b":
		return "[b]" + mybbChildren(n) + "[/b]"

	case "em":
		if mybbHasClass(n, "bbuline") {
			return "[u]" + mybbChildren(n) + "[/u]"
		}
		return "[i]" + mybbChildren(n) + "[/i]"

	case "i":
		return "[i]" + mybbChildren(n) + "[/i]"

	case "u":
		return "[u]" + mybbChildren(n) + "[/u]"

	case "del", "s", "strike":
		return "[s]" + mybbChildren(n) + "[/s]"

	case "a":
		href := mybbAttr(n, "href")
		if href != "" && mybbSafeURI(href) {
			return "[url=" + href + "]" + mybbChildren(n) + "[/url]"
		}
		return mybbChildren(n)

	case "img":
		src := mybbAttr(n, "src")
		if src != "" && mybbSafeURI(src) {
			return "[img]" + src + "[/img]"
		}
		return ""

	case "ul":
		return "[list]\n" + mybbChildren(n) + "[/list]\n"

	case "ol":
		return "[list=1]\n" + mybbChildren(n) + "[/list]\n"

	case "li":
		return "[*]" + strings.TrimSpace(mybbChildren(n)) + "\n"

	// Structural pass-throughs
	case "pre", "blockquote":
		return mybbChildren(n)

	case "cite":
		// Consumed by the parent quote-box handler; skip here
		return ""

	case "h1":
		return "[size=24][b]" + mybbChildren(n) + "[/b][/size]\n"
	case "h2":
		return "[size=20][b]" + mybbChildren(n) + "[/b][/size]\n"
	case "h3":
		return "[size=18][b]" + mybbChildren(n) + "[/b][/size]\n"
	case "h4", "h5", "h6":
		return "[size=16][b]" + mybbChildren(n) + "[/b][/size]\n"

	case "span":
		return mybbSpan(n)

	case "div":
		return mybbDiv(n)
	}

	return mybbChildren(n)
}

func mybbSpan(n *html.Node) string {
	style := mybbAttr(n, "style")
	inner := mybbChildren(n)

	if v := mybbStyle(style, "font-family"); v != "" {
		return "[font=" + v + "]" + inner + "[/font]"
	}
	if v := mybbStyle(style, "font-size"); v != "" {
		// Strip the "px" unit MyBB always appends
		size := strings.TrimSuffix(strings.TrimSpace(v), "px")
		return "[size=" + size + "]" + inner + "[/size]"
	}
	if v := mybbStyle(style, "color"); v != "" {
		return "[color=" + v + "]" + inner + "[/color]"
	}
	if mybbStyle(style, "font-style") == "italic" {
		return "[i]" + inner + "[/i]"
	}
	if v := mybbStyle(style, "text-align"); v != "" {
		return "[align=" + v + "]" + inner + "[/align]"
	}

	return inner
}

func mybbDiv(n *html.Node) string {
	switch {
	case mybbHasClass(n, "spoiler-box"):
		var title, content string
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if c.Data == "div" {
				title = strings.TrimSpace(mybbChildren(c))
			} else if c.Data == "blockquote" {
				content = strings.TrimSpace(mybbChildren(c))
			}
		}
		return "[spoiler=\"" + title + "\"]" + content + "[/spoiler]\n"

	case mybbHasClass(n, "quote-main") || mybbHasClass(n, "answer-box"):
		var author, content string
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if c.Data == "cite" {
				author = mybbCiteAuthor(mybbChildren(c))
			} else if c.Data == "blockquote" {
				content = strings.TrimSpace(mybbChildren(c))
			}
		}
		if author != "" {
			return "[quote=" + author + "]" + content + "[/quote]\n"
		}
		return "[quote]" + content + "[/quote]\n"

	case mybbHasClass(n, "code-box"):
		if pre := mybbFind(n, "pre"); pre != nil {
			return "[code]" + mybbChildren(pre) + "[/code]\n"
		}
		return mybbChildren(n)

	// Inner wrappers that belong to quote/code/spoiler boxes
	case mybbHasClass(n, "blockcode"), mybbHasClass(n, "scrollbox"):
		return mybbChildren(n)
	}

	return mybbChildren(n)
}

// ── helpers ──────────────────────────────────────────────────────────────────

func mybbAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func mybbHasClass(n *html.Node, cls string) bool {
	for _, c := range strings.Fields(mybbAttr(n, "class")) {
		if c == cls {
			return true
		}
	}
	return false
}

var mybbStyleRe = regexp.MustCompile(`(?i)([\w-]+)\s*:\s*([^;]+)`)

func mybbStyle(style, prop string) string {
	prop = strings.ToLower(prop)
	for _, m := range mybbStyleRe.FindAllStringSubmatch(style, -1) {
		if strings.ToLower(strings.TrimSpace(m[1])) == prop {
			return strings.TrimSpace(m[2])
		}
	}
	return ""
}

func mybbSafeURI(uri string) bool {
	lower := strings.ToLower(strings.TrimSpace(uri))
	return !strings.HasPrefix(lower, "javascript:") && !strings.HasPrefix(lower, "data:")
}

var mybbCiteRe = regexp.MustCompile(`(?i)#p\d+,(.+?)\s+написал|^(.+?)\s+(?:wrote|написал)`)

func mybbCiteAuthor(cite string) string {
	if m := mybbCiteRe.FindStringSubmatch(strings.TrimSpace(cite)); m != nil {
		if m[1] != "" {
			return strings.TrimSpace(m[1])
		}
		return strings.TrimSpace(m[2])
	}
	return ""
}

func mybbFind(n *html.Node, tag string) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == tag {
			return c
		}
		if found := mybbFind(c, tag); found != nil {
			return found
		}
	}
	return nil
}
