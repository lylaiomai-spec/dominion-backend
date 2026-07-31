package Services

import (
	"database/sql"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/frustra/bbcode"
)

var youtubeRegexp = regexp.MustCompile(`(?i)(?:youtube\.com/(?:watch\?(?:.*&)?v=|embed/|shorts/)|youtu\.be/)([a-zA-Z0-9_-]{11})`)

// autoLinkRe matches either a full HTML tag (kept as-is) or a raw http(s) URL (linkified).
// Matching tags first ensures URLs already inside href/src attributes are never touched.
var autoLinkRe = regexp.MustCompile(`(?i)(<[^>]*>)|(https?://[^\s<>"']+)`)

var (
	viewforumRe = regexp.MustCompile(`^/viewforum/(\d+)$`)
	loreRe      = regexp.MustCompile(`^/lore/(\d+)`)
	viewtopicRe = regexp.MustCompile(`^/viewtopic/(\d+)`)
	profileRe   = regexp.MustCompile(`^/profile/(\d+)$`)
	characterRe = regexp.MustCompile(`^/character/(\d+)$`)
)

func resolveInternalLinkLabel(rawURL, domain string, db *sql.DB) string {
	if domain == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	domainHost := strings.TrimPrefix(strings.TrimPrefix(domain, "https://"), "http://")
	domainHost = strings.SplitN(domainHost, "/", 2)[0]
	domainHost = strings.SplitN(domainHost, ":", 2)[0] // strip port if present
	if !strings.EqualFold(u.Hostname(), domainHost) {
		return ""
	}
	path := u.Path
	var name string
	if m := viewforumRe.FindStringSubmatch(path); m != nil {
		db.QueryRow("SELECT name FROM subforums WHERE id = ?", m[1]).Scan(&name)
	} else if m := loreRe.FindStringSubmatch(path); m != nil {
		db.QueryRow("SELECT name FROM topics WHERE id = ?", m[1]).Scan(&name)
	} else if m := viewtopicRe.FindStringSubmatch(path); m != nil {
		db.QueryRow("SELECT name FROM topics WHERE id = ?", m[1]).Scan(&name)
	} else if m := profileRe.FindStringSubmatch(path); m != nil {
		db.QueryRow("SELECT username FROM users WHERE id = ?", m[1]).Scan(&name)
	} else if m := characterRe.FindStringSubmatch(path); m != nil {
		db.QueryRow("SELECT name FROM character_base WHERE id = ?", m[1]).Scan(&name)
	} else {
		name = strings.ReplaceAll(strings.Trim(path, "/"), "-", " ")
	}
	return name
}

func LinkifyURLs(input, domain string, db *sql.DB) string {
	return autoLinkRe.ReplaceAllStringFunc(input, func(match string) string {
		if match[0] == '<' {
			return match
		}
		trimmed := strings.TrimRight(match, ".,;:!?)]}")
		suffix := match[len(trimmed):]
		label := resolveInternalLinkLabel(trimmed, domain, db)
		if label == "" {
			label = trimmed
		}
		return `<a href="` + html.EscapeString(trimmed) + `">` + html.EscapeString(label) + `</a>` + suffix
	})
}
var audioExtRegexp = regexp.MustCompile(`(?i)\.(mp3|ogg|wav|flac|aac|m4a|opus|webm)(\?.*)?$`)

func getArg(node *bbcode.BBCodeNode, key string) (string, bool) {
	val, ok := node.GetOpeningTag().Args[key]
	if !ok || val == "" {
		return "", false
	}
	return html.EscapeString(val), true
}

func getArgInt(node *bbcode.BBCodeNode, key string, min, max int) (int, bool) {
	val, ok := node.GetOpeningTag().Args[key]
	if !ok || val == "" {
		return 0, false
	}
	n, err := strconv.Atoi(val)
	if err != nil || n < min || n > max {
		return 0, false
	}
	return n, true
}

var bbCompiler = GetBBCompiler()

func ParseBBCode(text string) string {
	return bbCompiler.Compile(text)
}

func GetBBCompiler() bbcode.Compiler {
	compiler := bbcode.NewCompiler(true, true)

	compiler.SetTag("center", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "div"
		out.Attrs["style"] = "text-align: center;"
		return out, true
	})

	compiler.SetTag("left", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "div"
		out.Attrs["style"] = "text-align: left;"
		return out, true
	})

	compiler.SetTag("right", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "div"
		out.Attrs["style"] = "text-align: right;"
		return out, true
	})

	compiler.SetTag("align", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "div"

		value := node.GetOpeningTag().Value
		if value == "left" || value == "center" || value == "right" {
			out.Attrs["style"] = fmt.Sprintf("text-align: %s;", value)
		}

		return out, true
	})

	compiler.SetTag("div", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "div"

		if id, ok := getArg(node, "id"); ok {
			out.Attrs["id"] = id
		}
		if class, ok := getArg(node, "class"); ok {
			out.Attrs["class"] = class
		}

		var style string
		if border, ok := getArg(node, "border"); ok {
			style += fmt.Sprintf("border: %s;", border)
		}
		if width, ok := getArg(node, "width"); ok {
			style += fmt.Sprintf(" width: %s;", width)
		}
		if height, ok := getArg(node, "height"); ok {
			style += fmt.Sprintf(" height: %s;", height)
		}
		if style != "" {
			out.Attrs["style"] = style
		}

		return out, true
	})

	compiler.SetTag("size", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "span"

		value := node.GetOpeningTag().Value
		if size, err := strconv.Atoi(value); err == nil && size >= 0 && size <= 32 {
			out.Attrs["style"] = fmt.Sprintf("font-size: %dpx;", size)
		}

		return out, true
	})

	compiler.SetTag("font", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "span"

		validFonts := map[string]bool{
			// Generic families
			"serif": true, "sans-serif": true, "monospace": true,
			"cursive": true, "fantasy": true, "system-ui": true,
			// Web-safe fonts
			"arial": true, "helvetica": true, "verdana": true,
			"tahoma": true, "trebuchet ms": true, "gill sans": true,
			"times new roman": true, "georgia": true, "garamond": true, "palatino": true,
			"courier new": true, "lucida console": true, "monaco": true,
			"comic sans ms": true, "impact": true, "arial black": true,
		}

		value := node.GetOpeningTag().Value
		if validFonts[value] {
			out.Attrs["style"] = fmt.Sprintf("font-family: %s;", value)
		}

		return out, true
	})

	compiler.SetTag("grid", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "div"

		style := "display: grid;"
		if cols, ok := getArgInt(node, "columns", 1, 12); ok {
			style += fmt.Sprintf(" grid-template-columns: repeat(%d, 1fr);", cols)
		}
		if rows, ok := getArgInt(node, "rows", 1, 12); ok {
			style += fmt.Sprintf(" grid-template-rows: repeat(%d, 1fr);", rows)
		}
		if gap, ok := getArg(node, "gap"); ok {
			style += fmt.Sprintf(" gap: %s;", gap)
		}
		if width, ok := getArg(node, "width"); ok {
			style += fmt.Sprintf(" width: %s;", width)
		}

		out.Attrs["style"] = style
		return out, true
	})

	compiler.SetTag("grid-item", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "div"

		var style string
		if col, ok := getArgInt(node, "col", 1, 12); ok {
			style += fmt.Sprintf("grid-column-start: %d;", col)
		}
		if row, ok := getArgInt(node, "row", 1, 12); ok {
			style += fmt.Sprintf(" grid-row-start: %d;", row)
		}
		if colSpan, ok := getArgInt(node, "col-span", 1, 12); ok {
			style += fmt.Sprintf(" grid-column-end: span %d;", colSpan)
		}
		if rowSpan, ok := getArgInt(node, "row-span", 1, 12); ok {
			style += fmt.Sprintf(" grid-row-end: span %d;", rowSpan)
		}
		if style != "" {
			out.Attrs["style"] = style
		}

		return out, true
	})

	compiler.SetTag("insert-post", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "post-insert"

		value := node.GetOpeningTag().Value
		id, err := strconv.Atoi(value)
		if err != nil || id <= 0 {
			return out, false
		}

		out.Attrs["data-insert"] = strconv.Itoa(id)

		return out, false
	})

	compiler.SetTag("float", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "div"

		value := node.GetOpeningTag().Value
		if value == "right" || value == "left" {
			out.Attrs["style"] = fmt.Sprintf("float: %s;", value)
		}

		return out, true
	})

	compiler.SetTag("ul", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "ul"
		return out, true
	})

	compiler.SetTag("ol", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "ol"
		return out, true
	})

	compiler.SetTag("li", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "li"
		return out, true
	})

	compiler.SetTag("spoiler", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "spoiler-box"

		title := "Spoiler"
		if raw := node.GetOpeningTag().Raw; strings.Contains(raw, "=") {
			idx := strings.Index(raw, "=")
			title = strings.TrimSpace(strings.TrimSuffix(raw[idx+1:], "]"))
		}
		if title == "" {
			title = "Spoiler"
		}

		out.Attrs["data-title"] = html.EscapeString(title)

		return out, true
	})

	compiler.SetTag("audio", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		url := strings.TrimSpace(bbcode.CompileText(node))
		if !audioExtRegexp.MatchString(url) {
			return out, false
		}
		out.Name = "audio"
		out.Attrs["src"] = html.EscapeString(url)
		out.Attrs["controls"] = "true"
		out.Attrs["preload"] = "metadata"
		return out, true
	})

	compiler.SetTag("video", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		url := strings.TrimSpace(bbcode.CompileText(node))
		matches := youtubeRegexp.FindStringSubmatch(url)
		if len(matches) < 2 {
			return out, false
		}
		videoID := html.EscapeString(matches[1])
		out.Name = "iframe"
		out.Attrs["src"] = "https://www.youtube.com/embed/" + videoID
		out.Attrs["allowfullscreen"] = "true"
		out.Attrs["frameborder"] = "0"
		out.Attrs["allow"] = "accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture"
		return out, true
	})

	compiler.SetTag("img", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "img"

		value := node.GetOpeningTag().Value
		if value == "" {
			out.Attrs["src"] = bbcode.ValidURL(bbcode.CompileText(node))
		} else {
			out.Attrs["src"] = bbcode.ValidURL(value)
			text := bbcode.CompileText(node)
			if len(text) > 0 {
				out.Attrs["alt"] = text
				out.Attrs["title"] = text
			}
		}
		out.Attrs["loading"] = "lazy"
		out.Attrs["referrerpolicy"] = "no-referrer"

		return out, false
	})

	return compiler
}
