package knowledge

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"alpha-nexus-egolite/internal/db"
)

const (
	chunkTargetRunes = 900
	chunkMaxRunes    = 1400
	chunkOverlap     = 120
)

var markdownHeadingPattern = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)

// ChunkMarkdown uses headings as semantic boundaries and only falls back to
// a small rune overlap when a single section is too large.
func ChunkMarkdown(markdown string) []db.KnowledgeChunkInput {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	headings := make([]string, 6)
	currentHeading := ""
	var section []string
	var chunks []db.KnowledgeChunkInput
	flush := func() {
		body := strings.TrimSpace(strings.Join(section, "\n"))
		section = section[:0]
		if body == "" {
			return
		}
		for _, part := range splitBounded(body) {
			chunks = append(chunks, db.KnowledgeChunkInput{
				Ordinal:     len(chunks),
				HeadingPath: currentHeading,
				Content:     part,
			})
		}
	}
	for _, line := range lines {
		match := markdownHeadingPattern.FindStringSubmatch(line)
		if len(match) == 3 {
			flush()
			level := len(match[1])
			headings[level-1] = cleanMarkdownHeading(match[2])
			for index := level; index < len(headings); index++ {
				headings[index] = ""
			}
			path := make([]string, 0, level)
			for index := 0; index < level; index++ {
				if headings[index] != "" {
					path = append(path, headings[index])
				}
			}
			currentHeading = strings.Join(path, " / ")
			continue
		}
		section = append(section, line)
	}
	flush()
	return chunks
}

func cleanMarkdownHeading(value string) string {
	const escapable = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"
	value = strings.TrimSpace(value)
	var cleaned strings.Builder
	cleaned.Grow(len(value))
	for index := 0; index < len(value); index++ {
		if value[index] == '\\' && index+1 < len(value) && strings.ContainsRune(escapable, rune(value[index+1])) {
			index++
		}
		cleaned.WriteByte(value[index])
	}
	return strings.TrimSpace(cleaned.String())
}

func splitBounded(body string) []string {
	if utf8.RuneCountInString(body) <= chunkMaxRunes {
		return []string{body}
	}
	paragraphs := strings.Split(body, "\n\n")
	var output []string
	current := ""
	for _, paragraph := range paragraphs {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}
		candidate := paragraph
		if current != "" {
			candidate = current + "\n\n" + paragraph
		}
		if utf8.RuneCountInString(candidate) <= chunkTargetRunes {
			current = candidate
			continue
		}
		if current != "" {
			output = append(output, current)
			current = ""
		}
		if utf8.RuneCountInString(paragraph) > chunkMaxRunes {
			output = append(output, splitLongRunes(paragraph)...)
		} else {
			current = paragraph
		}
	}
	if current != "" {
		output = append(output, current)
	}
	return output
}

func splitLongRunes(text string) []string {
	runes := []rune(text)
	var output []string
	for start := 0; start < len(runes); {
		end := start + chunkMaxRunes
		if end > len(runes) {
			end = len(runes)
		}
		output = append(output, strings.TrimSpace(string(runes[start:end])))
		if end == len(runes) {
			break
		}
		start = end - chunkOverlap
	}
	return output
}
