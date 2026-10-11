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
var soundcloudRe = regexp.MustCompile(`(?i)^https?://soundcloud\.com/`)
var spotifyRe = regexp.MustCompile(`(?i)open\.spotify\.com/(track|album|playlist|episode)/([a-zA-Z0-9]+)`)
var archiveRe = regexp.MustCompile(`(?i)archive\.org/(?:details|embed)/([^/?#\s]+)`)
var bandcampEmbedRe = regexp.MustCompile(`(?i)^https?://bandcamp\.com/EmbeddedPlayer/`)

// autoLinkRe matches either a full HTML tag (kept as-is) or a raw http(s) URL (linkified).
// Matching tags first ensures URLs already inside href/src attributes are never touched.
var autoLinkRe = regexp.MustCompile(`(?i)(<[^>]*>)|(https?://[^\s<>"']+)`)
var preBlockRe = regexp.MustCompile(`(?is)<pre[^>]*>.*?</pre>`)

var (
	viewforumRe = regexp.MustCompile(`^/viewforum/(\d+)$`)
	loreRe      = regexp.MustCompile(`^/lore/(\d+)`)
	viewtopicRe = regexp.MustCompile(`^/viewtopic/(\d+)`)
	profileRe   = regexp.MustCompile(`^/profile/(\d+)$`)
	characterRe = regexp.MustCompile(`^/character/(\d+)$`)
	arcRe       = regexp.MustCompile(`^/arc/(\d+)$`)
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
	} else if m := arcRe.FindStringSubmatch(path); m != nil {
		db.QueryRow("SELECT title FROM arcs WHERE id = ?", m[1]).Scan(&name)
	} else {
		name = strings.ReplaceAll(strings.Trim(path, "/"), "-", " ")
	}
	return name
}

func LinkifyURLs(input, domain string, db *sql.DB) string {
	placeholders := make(map[string]string)
	i := 0
	input = preBlockRe.ReplaceAllStringFunc(input, func(block string) string {
		key := fmt.Sprintf("\x00pre%d\x00", i)
		i++
		placeholders[key] = block
		return key
	})

	result := autoLinkRe.ReplaceAllStringFunc(input, func(match string) string {
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

	for key, block := range placeholders {
		result = strings.ReplaceAll(result, key, block)
	}
	return result
}
var audioExtRegexp = regexp.MustCompile(`(?i)\.(mp3|ogg|wav|flac|aac|m4a|opus|webm)(\?.*)?$`)
var cssDimensionRe = regexp.MustCompile(`^[a-zA-Z0-9.%\-]+$`)

// getRawArg extracts a named arg directly from the raw tag string, bypassing
// the bbcode library's quoted-value parser which corrupts multi-byte UTF-8.
func getRawArg(node *bbcode.BBCodeNode, key string) (string, bool) {
	raw := node.GetOpeningTag().Raw
	search := key + "="
	idx := strings.Index(strings.ToLower(raw), search)
	if idx < 0 {
		return "", false
	}
	rest := raw[idx+len(search):]
	if len(rest) == 0 {
		return "", false
	}
	var val string
	if rest[0] == '"' || rest[0] == '\'' {
		quoteChar := rest[0]
		end := strings.IndexByte(rest[1:], quoteChar)
		if end < 0 {
			return "", false
		}
		val = rest[1 : end+1]
	} else {
		end := strings.IndexAny(rest, " \t]")
		if end < 0 {
			val = rest
		} else {
			val = rest[:end]
		}
	}
	if val == "" {
		return "", false
	}
	return val, true
}

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

var hideBlockRe = regexp.MustCompile(`(?s)<hide-block data-users="([^"]*)">(.*?)</hide-block>`)

func isArcGMForTopic(userID, topicID int, db *sql.DB) bool {
	if userID == 0 || topicID == 0 {
		return false
	}
	var count int
	db.QueryRow(`
		SELECT COUNT(*) FROM arc_game_masters agm
		JOIN arc_episodes ae ON ae.arc_id = agm.arc_id
		JOIN episode_base eb ON eb.id = ae.episode_id
		WHERE eb.topic_id = ? AND agm.user_id = ?`, topicID, userID).Scan(&count)
	if count > 0 {
		return true
	}
	db.QueryRow(`
		SELECT COUNT(*) FROM arcs a
		JOIN arc_episodes ae ON ae.arc_id = a.id
		JOIN episode_base eb ON eb.id = ae.episode_id
		WHERE eb.topic_id = ? AND a.creator_id = ?`, topicID, userID).Scan(&count)
	return count > 0
}

func ProcessHideBlocks(content string, userID int, topicID int, db *sql.DB) string {
	isGM := isArcGMForTopic(userID, topicID, db)
	return hideBlockRe.ReplaceAllStringFunc(content, func(match string) string {
		sub := hideBlockRe.FindStringSubmatch(match)
		if len(sub) < 3 {
			return ""
		}

		allowed := isGM
		var userIDs []int
		for _, s := range strings.Split(sub[1], ",") {
			s = strings.TrimSpace(s)
			if id, err := strconv.Atoi(s); err == nil {
				userIDs = append(userIDs, id)
				if id == userID {
					allowed = true
				}
			}
		}
		if !allowed {
			return ""
		}

		// Fetch usernames for the header
		placeholders := strings.Repeat("?,", len(userIDs)-1) + "?"
		args := make([]interface{}, len(userIDs))
		for i, id := range userIDs {
			args[i] = id
		}
		type userLink struct{ id int; username string }
		var users []userLink
		if rows, err := db.Query("SELECT id, username FROM users WHERE id IN ("+placeholders+")", args...); err == nil {
			defer rows.Close()
			for rows.Next() {
				var u userLink
				if rows.Scan(&u.id, &u.username) == nil {
					users = append(users, u)
				}
			}
		}

		var sb strings.Builder
		sb.WriteString(`<div class="hide-block">`)
		sb.WriteString(`<div class="hide-header">Visible to: `)
		for i, u := range users {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(`<a class="hide-user-link" href="/profile/`)
			sb.WriteString(strconv.Itoa(u.id))
			sb.WriteString(`" target="_blank">`)
			sb.WriteString(html.EscapeString(u.username))
			sb.WriteString(`</a>`)
		}
		sb.WriteString(`</div><div class="hide-body">`)
		sb.WriteString(sub[2])
		sb.WriteString(`</div></div>`)
		return sb.String()
	})
}

var npcLookupRe = regexp.MustCompile(`<npc-lookup data-id="(\d+)">(?:</npc-lookup>)?`)

func ResolveNPCPlaceholders(content string, db *sql.DB) string {
	matches := npcLookupRe.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return content
	}

	ids := make([]interface{}, 0, len(matches))
	seen := make(map[string]bool)
	for _, m := range matches {
		if !seen[m[1]] {
			ids = append(ids, m[1])
			seen[m[1]] = true
		}
	}

	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	rows, err := db.Query("SELECT id, name, avatar FROM npc WHERE id IN ("+placeholders+")", ids...)
	if err != nil {
		return content
	}
	defer rows.Close()

	type npcData struct{ name, avatar string }
	npcs := make(map[string]npcData)
	for rows.Next() {
		var id int
		var name string
		var avatar sql.NullString
		if rows.Scan(&id, &name, &avatar) == nil {
			n := npcData{name: name}
			if avatar.Valid {
				n.avatar = avatar.String
			}
			npcs[strconv.Itoa(id)] = n
		}
	}

	return npcLookupRe.ReplaceAllStringFunc(content, func(match string) string {
		sub := npcLookupRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		npc, ok := npcs[sub[1]]
		if !ok {
			return ""
		}
		var sb strings.Builder
		sb.WriteString(`<a class="npc-link" href="/npc/`)
		sb.WriteString(sub[1])
		sb.WriteString(`" target="_blank">`)
		if npc.avatar != "" {
			sb.WriteString(`<img class="npc-avatar" src="`)
			sb.WriteString(html.EscapeString(npc.avatar))
			sb.WriteString(`" alt="`)
			sb.WriteString(html.EscapeString(npc.name))
			sb.WriteString(`">`)
		}
		sb.WriteString(`<span class="npc-name">`)
		sb.WriteString(html.EscapeString(npc.name))
		sb.WriteString(`</span></a>`)
		return sb.String()
	})
}

// collectNpcIDs recursively walks a BBCode node tree and returns the id
// attribute of every [npc] tag found, in document order. This is needed
// because [npc id=1][npc id=2] (without explicit closing tags) causes the
// parser to nest id=2 inside id=1, so a flat children scan would miss it.
func collectNpcIDs(node *bbcode.BBCodeNode) []string {
	var ids []string
	for _, child := range node.Children {
		if openTag := child.GetOpeningTag(); openTag != nil && openTag.Name == "npc" {
			if idStr, ok := getRawArg(child, "id"); ok {
				if _, err := strconv.Atoi(idStr); err == nil {
					ids = append(ids, idStr)
				}
			}
		}
		ids = append(ids, collectNpcIDs(child)...)
	}
	return ids
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
		if validFonts[strings.ToLower(value)] {
			out.Attrs["style"] = fmt.Sprintf("font-family: %s;", value)
		}

		return out, true
	})

	compiler.SetTag("grid", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "div"

		out.Attrs["class"] = "grid"
		style := "display: grid; max-width: 100%;"
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

		style := "min-width: 0;"
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

	compiler.SetTag("list", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
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

	compiler.SetTag("*", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
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
		rawURL := strings.TrimSpace(bbcode.CompileText(node))

		sourceSite, hasSourceSite := getRawArg(node, "source_site")
		if !hasSourceSite {
			// File-based audio (<audio> element)
			if !audioExtRegexp.MatchString(rawURL) {
				return out, false
			}
			out.Name = "audio"
			out.Attrs["src"] = html.EscapeString(rawURL)
			out.Attrs["controls"] = "true"
			out.Attrs["preload"] = "metadata"
			return out, true
		}

		// Streaming service embeds (<iframe>)
		type embedCfg struct {
			src    string
			height string
			allow  string
		}
		var cfg embedCfg

		switch strings.ToLower(sourceSite) {
		case "soundcloud":
			if !soundcloudRe.MatchString(rawURL) {
				return out, false
			}
			cfg = embedCfg{
				src:    "https://w.soundcloud.com/player/?url=" + url.QueryEscape(rawURL),
				height: "166",
				allow:  "autoplay",
			}
		case "spotify":
			m := spotifyRe.FindStringSubmatch(rawURL)
			if len(m) < 3 {
				return out, false
			}
			cfg = embedCfg{
				src:    "https://open.spotify.com/embed/" + m[1] + "/" + m[2],
				height: "152",
				allow:  "autoplay; clipboard-write; encrypted-media; fullscreen; picture-in-picture",
			}
		case "archive":
			m := archiveRe.FindStringSubmatch(rawURL)
			if len(m) < 2 {
				return out, false
			}
			cfg = embedCfg{
				src:    "https://archive.org/embed/" + m[1],
				height: "60",
				allow:  "autoplay",
			}
		case "bandcamp":
			if !bandcampEmbedRe.MatchString(rawURL) {
				return out, false
			}
			cfg = embedCfg{
				src:    rawURL,
				height: "120",
				allow:  "autoplay",
			}
		default:
			return out, false
		}

		out.Name = "iframe"
		out.Attrs["src"] = html.EscapeString(cfg.src)
		out.Attrs["width"] = "100%"
		out.Attrs["height"] = cfg.height
		out.Attrs["frameborder"] = "0"
		out.Attrs["allow"] = cfg.allow
		out.Attrs["loading"] = "lazy"
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

	compiler.SetTag("code", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "pre"
		for _, child := range node.Children {
			out.AppendChild(bbcode.CompileRaw(child))
		}
		// Ensure closing </pre> is emitted even for empty blocks.
		// HTMLTag.Compile only adds a closing tag when children are present.
		if len(node.Children) == 0 {
			out.AppendChild(bbcode.NewHTMLTag(""))
		}
		return out, false
	})

	compiler.SetTag("table", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "table"
		out.Attrs["style"] = "border-collapse: collapse;"
		return out, true
	})

	compiler.SetTag("tr", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "tr"
		return out, true
	})

	compiler.SetTag("td", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "td"
		if width, ok := getRawArg(node, "width"); ok && cssDimensionRe.MatchString(width) {
			out.Attrs["style"] = fmt.Sprintf("width: %s;", width)
		}
		if colspan, ok := getArgInt(node, "colspan", 1, 100); ok {
			out.Attrs["colspan"] = strconv.Itoa(colspan)
		}
		if rowspan, ok := getArgInt(node, "rowspan", 1, 100); ok {
			out.Attrs["rowspan"] = strconv.Itoa(rowspan)
		}
		return out, true
	})

	compiler.SetTag("quote", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "blockquote"

		// Extract the tag value (author name), stripping surrounding quotes if present.
		author := node.GetOpeningTag().Value
		if raw := node.GetOpeningTag().Raw; strings.Contains(raw, "=") {
			rest := raw[strings.Index(raw, "=")+1:]
			if len(rest) > 0 && (rest[0] == '"' || rest[0] == '\'') {
				quoteChar := rest[0]
				if end := strings.IndexByte(rest[1:], quoteChar); end >= 0 {
					author = rest[1 : end+1]
				}
			}
		}

		var href string
		topicID, hasTopicID := getRawArg(node, "topic-id")
		postID, hasPostID := getRawArg(node, "post-id")
		if author != "" && hasTopicID && hasPostID {
			if _, err := strconv.Atoi(topicID); err == nil {
				if _, err := strconv.Atoi(postID); err == nil {
					href = "/viewtopic/" + topicID + "?post_id=" + postID
				}
			}
		}

		cite := bbcode.NewHTMLTag("")
		cite.Name = "cite"
		if author != "" {
			if href != "" {
				link := bbcode.NewHTMLTag("")
				link.Name = "a"
				link.Attrs["href"] = href
				link.AppendChild(bbcode.NewHTMLTag(author))
				cite.AppendChild(link)
				cite.AppendChild(bbcode.NewHTMLTag(" said:"))
			} else {
				cite.AppendChild(bbcode.NewHTMLTag(author + " said:"))
			}
		} else {
			cite.AppendChild(bbcode.NewHTMLTag("Quote"))
		}

		return out.AppendChild(cite), true
	})

	compiler.SetTag("url", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "a"
		value := node.GetOpeningTag().Value
		if value == "" {
			text := bbcode.CompileText(node)
			if len(text) > 0 {
				out.Attrs["href"] = bbcode.ValidURL(text)
			}
		} else {
			out.Attrs["href"] = bbcode.ValidURL(value)
		}
		if target, ok := getRawArg(node, "target"); ok && target == "_blank" {
			out.Attrs["target"] = "_blank"
		}
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
		if title, ok := getRawArg(node, "title"); ok {
			out.Attrs["title"] = title
		}
		if width, ok := getArgInt(node, "width", 1, 9999); ok {
			out.Attrs["width"] = strconv.Itoa(width)
		}
		if height, ok := getArgInt(node, "height", 1, 9999); ok {
			out.Attrs["height"] = strconv.Itoa(height)
		}
		out.Attrs["loading"] = "lazy"
		out.Attrs["referrerpolicy"] = "no-referrer"

		return out, false
	})

	compiler.SetTag("hide", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "hide-block"
		if users, ok := getRawArg(node, "users"); ok {
			// Allow only digits and commas
			safe := true
			for _, ch := range users {
				if (ch < '0' || ch > '9') && ch != ',' {
					safe = false
					break
				}
			}
			if safe {
				out.Attrs["data-users"] = users
			}
		}
		return out, true
	})

	compiler.SetTag("npc-block", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "div"
		out.Attrs["class"] = "npc-block"
		return out, true
	})

	compiler.SetTag("npc-header", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "div"
		out.Attrs["class"] = "npc-header"
		for _, idStr := range collectNpcIDs(node) {
			lookup := bbcode.NewHTMLTag("")
			lookup.Name = "npc-lookup"
			lookup.Attrs["data-id"] = idStr
			out.AppendChild(lookup)
		}
		return out, false
	})

	compiler.SetTag("npc-body", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "div"
		out.Attrs["class"] = "npc-body"
		return out, true
	})

	compiler.SetTag("npc", func(node *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "npc-lookup"
		if idStr, ok := getRawArg(node, "id"); ok {
			if _, err := strconv.Atoi(idStr); err == nil {
				out.Attrs["data-id"] = idStr
			}
		}
		return out, false
	})

	return compiler
}
