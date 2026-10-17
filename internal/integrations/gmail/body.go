/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package gmail

import (
	"bytes"
	"io"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// Attachment is a file attached to a message.
type Attachment struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	MimeType string `json:"mimeType"`
	Size     int    `json:"size"`
}

// extract returns the readable body of a message part tree and its attachments.
// It prefers text/plain and falls back to the text of the text/html parts.
func extract(root *Part) (body string, atts []Attachment) {
	var plain, htm []string
	var walk func(p *Part)
	walk = func(p *Part) {
		switch {
		case p.Filename != "" && p.Body.AttachmentID != "":
			atts = append(atts, Attachment{p.Body.AttachmentID, p.Filename, p.MimeType, p.Body.Size})
		case p.MimeType == "text/plain" && p.Body.Data != "":
			plain = append(plain, decodeText(p))
		case p.MimeType == "text/html" && p.Body.Data != "":
			htm = append(htm, decodeText(p))
		}
		for i := range p.Parts {
			walk(&p.Parts[i])
		}
	}
	walk(root)
	if s := strings.TrimSpace(strings.Join(plain, "\n\n")); s != "" {
		return s, atts
	}
	return htmlText(strings.Join(htm, "\n")), atts
}

// decodeText decodes a part's base64url data and converts it from its declared charset to UTF-8.
func decodeText(p *Part) string {
	raw, err := decodeB64(p.Body.Data)
	if err != nil {
		return ""
	}
	r, err := charset.NewReader(bytes.NewReader(raw), p.header("Content-Type"))
	if err != nil {
		return strings.ToValidUTF8(string(raw), "")
	}
	out, err := io.ReadAll(r)
	if err != nil {
		return strings.ToValidUTF8(string(raw), "")
	}
	return string(out)
}

var blockTags = map[string]bool{"p": true, "div": true, "br": true, "tr": true, "li": true, "h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true, "table": true, "ul": true, "ol": true, "hr": true, "pre": true}

// htmlText renders HTML as plain text. Scripts, styles and quoted replies
// (blockquote, Gmail's gmail_quote div) are dropped.
func htmlText(src string) string {
	z := html.NewTokenizer(strings.NewReader(src))
	var b strings.Builder
	skip, depth := "", 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		switch tt {
		case html.TextToken:
			if skip == "" {
				b.WriteString(string(z.Text()))
			}
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			if skip != "" {
				if tag == skip && tt == html.StartTagToken {
					depth++
				} else if tag == skip && tt == html.EndTagToken {
					if depth--; depth == 0 {
						skip = ""
					}
				}
				continue
			}
			if tt == html.StartTagToken {
				switch tag {
				case "script", "style", "head", "blockquote":
					skip, depth = tag, 1
				case "div":
					for hasAttr {
						var k, v []byte
						k, v, hasAttr = z.TagAttr()
						if string(k) == "class" && strings.Contains(string(v), "gmail_quote") {
							skip, depth = "div", 1
						}
					}
				}
			}
			if blockTags[tag] {
				b.WriteByte('\n')
			}
		}
	}
	return tidy(b.String())
}

// tidy trims each line, collapses runs of spaces and caps blank lines at one.
func tidy(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), " ", " ")
	var out []string
	blank := false
	for _, line := range strings.Split(s, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			if !blank && len(out) > 0 {
				out = append(out, "")
			}
			blank = true
			continue
		}
		blank = false
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

var (
	quoteIntro   = regexp.MustCompile(`^On .{1,300}wrote:\s*$`)
	quoteIntroLo = regexp.MustCompile(`^(?i)-{2,}\s*(original message|forwarded message)|^_{10,}$`)
	headerBlock  = regexp.MustCompile(`^(?i)(sent|date):\s`)
)

// trimQuoted drops the quoted reply history: "> " lines and everything from
// an "On ... wrote:" line, an Original Message rule or an Outlook From/Sent block.
func trimQuoted(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var keep []string
	for i, line := range lines {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, ">"):
			continue
		case quoteIntro.MatchString(t), quoteIntroLo.MatchString(t):
			return strings.TrimSpace(strings.Join(keep, "\n"))
		case strings.HasPrefix(t, "On ") && i+1 < len(lines) && quoteIntro.MatchString(t+" "+strings.TrimSpace(lines[i+1])):
			return strings.TrimSpace(strings.Join(keep, "\n"))
		case strings.HasPrefix(strings.ToLower(t), "from:") && i+1 < len(lines) && headerBlock.MatchString(strings.TrimSpace(lines[i+1])) &&
			(i == 0 || strings.TrimSpace(lines[i-1]) == ""):
			return strings.TrimSpace(strings.Join(keep, "\n"))
		}
		keep = append(keep, line)
	}
	return strings.TrimSpace(strings.Join(keep, "\n"))
}

// clip cuts s to n characters and says so.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "\n[truncated: " + strconv.Itoa(len(r)) + " characters in total]"
}
