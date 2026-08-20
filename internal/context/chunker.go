package context

import "strings"

// Chunker splits normalized text using rune counts and an overlap. It prefers
// newline/word boundaries while guaranteeing forward progress.
type Chunker struct {
	Size    int
	Overlap int
}

func (c Chunker) Split(text string) []string {
	size := c.Size
	if size <= 0 {
		size = 1200
	}
	overlap := c.Overlap
	if overlap < 0 || overlap >= size {
		overlap = size / 8
	}
	runes := []rune(strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n")))
	var chunks []string
	for start := 0; start < len(runes); {
		end := start + size
		if end > len(runes) {
			end = len(runes)
		} else {
			floor := start + size/2
			for i := end; i > floor; i-- {
				if runes[i-1] == '\n' || runes[i-1] == ' ' {
					end = i
					break
				}
			}
		}
		part := strings.TrimSpace(string(runes[start:end]))
		if part != "" {
			chunks = append(chunks, part)
		}
		if end == len(runes) {
			break
		}
		next := end - overlap
		if next <= start {
			next = end
		}
		start = next
	}
	return chunks
}
