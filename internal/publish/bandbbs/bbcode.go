package bandbbs

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var (
	markdownImageRe   = regexp.MustCompile(`!\[([^\]]*)\]\((https?://[^)\s]+)\)`)
	markdownLinkRe    = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^)\s]+)\)`)
	markdownCodeRe    = regexp.MustCompile("`([^`\\n]+)`")
	markdownBoldRe    = regexp.MustCompile(`\*\*([^\n]+?)\*\*|__([^\n]+?)__`)
	markdownItalicRe  = regexp.MustCompile(`(?:\*([^*\n]+)\*|_([^_\n]+)_)`)
	markdownHeadingRe = regexp.MustCompile(`^\s*#{1,6}\s+(.+?)\s*$`)
	markdownQuoteRe   = regexp.MustCompile(`^\s*>\s?(.*)$`)
	markdownBulletRe  = regexp.MustCompile(`^\s*[-*+]\s+(.+?)\s*$`)
	markdownOrderedRe = regexp.MustCompile(`^\s*\d+[.)]\s+(.+?)\s*$`)
)

// MarkdownToBBCode converts the creator's GFM subset into XenForo BBCode.
// Source text is treated as Markdown, so bracket sequences are escaped instead
// of allowing arbitrary forum/custom BBCode to pass through.
func MarkdownToBBCode(value string) string {
	value = strings.ReplaceAll(value, "\x00", "")
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(strings.TrimSpace(value), "\n")
	var out []string
	inCode := false
	var codeLines []string
	flushCode := func() {
		if len(codeLines) == 0 {
			out = append(out, "[CODE][/CODE]")
		} else {
			out = append(out, "[CODE]"+escapeBBCodeText(strings.Join(codeLines, "\n"))+"[/CODE]")
		}
		codeLines = nil
	}
	for i := 0; i < len(lines); {
		line := lines[i]
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if inCode {
				flushCode()
				inCode = false
			} else {
				inCode = true
			}
			i++
			continue
		}
		if inCode {
			codeLines = append(codeLines, line)
			i++
			continue
		}
		if markdownBulletRe.MatchString(line) || markdownOrderedRe.MatchString(line) {
			items := make([]string, 0)
			for i < len(lines) {
				match := markdownBulletRe.FindStringSubmatch(lines[i])
				if len(match) == 0 {
					match = markdownOrderedRe.FindStringSubmatch(lines[i])
				}
				if len(match) == 0 {
					break
				}
				items = append(items, "[*]"+convertInlineMarkdown(match[1]))
				i++
			}
			out = append(out, "[LIST]"+strings.Join(items, "")+"[/LIST]")
			continue
		}
		if match := markdownHeadingRe.FindStringSubmatch(line); len(match) > 0 {
			out = append(out, "[B]"+convertInlineMarkdown(match[1])+"[/B]")
		} else if match := markdownQuoteRe.FindStringSubmatch(line); len(match) > 0 {
			out = append(out, "[QUOTE]"+convertInlineMarkdown(match[1])+"[/QUOTE]")
		} else {
			out = append(out, convertInlineMarkdown(line))
		}
		i++
	}
	if inCode {
		flushCode()
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func escapeBBCodeText(value string) string {
	return strings.NewReplacer("[", "&#91;", "]", "&#93;").Replace(value)
}

func safeBBCodeURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && !strings.ContainsAny(value, "[]\"'\r\n")
}

func convertInlineMarkdown(value string) string { return convertInlineMarkdownAt(value, 0) }
func convertInlineMarkdownAt(value string, depth int) string {
	if depth >= 16 {
		return escapeBBCodeText(value)
	}
	placeholders := []string{}
	put := func(rendered string) string {
		key := fmt.Sprintf("\x00%d\x00", len(placeholders))
		placeholders = append(placeholders, rendered)
		return key
	}
	value = markdownCodeRe.ReplaceAllStringFunc(value, func(match string) string {
		parts := markdownCodeRe.FindStringSubmatch(match)
		return put("[CODE]" + escapeBBCodeText(parts[1]) + "[/CODE]")
	})
	value = markdownImageRe.ReplaceAllStringFunc(value, func(match string) string {
		parts := markdownImageRe.FindStringSubmatch(match)
		if !safeBBCodeURL(parts[2]) {
			return escapeBBCodeText(match)
		}
		return put("[IMG]" + parts[2] + "[/IMG]")
	})
	value = markdownLinkRe.ReplaceAllStringFunc(value, func(match string) string {
		parts := markdownLinkRe.FindStringSubmatch(match)
		if !safeBBCodeURL(parts[2]) {
			return escapeBBCodeText(match)
		}
		return put("[URL=" + parts[2] + "]" + convertInlineMarkdownAt(parts[1], depth+1) + "[/URL]")
	})
	value = markdownBoldRe.ReplaceAllStringFunc(value, func(match string) string {
		parts := markdownBoldRe.FindStringSubmatch(match)
		text := parts[1]
		if text == "" {
			text = parts[2]
		}
		return put("[B]" + convertInlineMarkdownAt(text, depth+1) + "[/B]")
	})
	value = markdownItalicRe.ReplaceAllStringFunc(value, func(match string) string {
		parts := markdownItalicRe.FindStringSubmatch(match)
		text := parts[1]
		if text == "" {
			text = parts[2]
		}
		return put("[I]" + convertInlineMarkdownAt(text, depth+1) + "[/I]")
	})
	value = escapeBBCodeText(value)
	for i := len(placeholders) - 1; i >= 0; i-- {
		key := fmt.Sprintf("\x00%d\x00", i)
		value = strings.ReplaceAll(value, key, placeholders[i])
	}
	return value
}

var bbcodeImageURLRe = regexp.MustCompile(`(?i)\[IMG\](https?://[^\s\[\]]+)\[/IMG\]`)

// Only approved snapshot media may become forum images; no arbitrary URL fetch.
func rewriteAttachedImages(value string, attachments map[string]string) (string, error) {
	var invalid bool
	value = bbcodeImageURLRe.ReplaceAllStringFunc(value, func(match string) string {
		raw := bbcodeImageURLRe.FindStringSubmatch(match)[1]
		u, err := url.Parse(raw)
		if err != nil {
			invalid = true
			return match
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) != 4 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "blobs" {
			invalid = true
			return match
		}
		target, ok := attachments[parts[3]]
		if !ok {
			invalid = true
			return match
		}
		return "[IMG]" + target + "[/IMG]"
	})
	if invalid {
		return "", fmt.Errorf("inline image must reference an image included in the approved resource snapshot")
	}
	return value, nil
}
