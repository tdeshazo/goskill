package skills

import "strings"

// markdownReference is a destination found by the bounded Markdown parser.
// Line and column point at the first character of the destination so callers
// can report a useful location without exposing parser implementation details.
type markdownReference struct {
	destination string
	line        int
	column      int
}

// parseMarkdownReferences extracts inline link/image destinations and link
// definition destinations. It intentionally does not attempt to parse all of
// CommonMark: fenced and inline code are ignored, and malformed constructs are
// left alone so validation does not turn prose into filesystem diagnostics.
func parseMarkdownReferences(raw string) []markdownReference {
	sourceLines := splitLines(raw)
	bodyStart := markdownBodyStart(sourceLines)
	lines := splitLines(maskInlineCode(strings.Join(sourceLines[bodyStart:], "\n")))
	references := make([]markdownReference, 0)
	inFence := false
	var fenceChar byte
	fenceLength := 0
	for lineIndex := bodyStart; lineIndex < len(sourceLines); lineIndex++ {
		line := lines[lineIndex-bodyStart]
		if marker, length, ok := markdownFence(line); ok {
			if !inFence {
				inFence = true
				fenceChar = marker
				fenceLength = length
				continue
			}
			if marker == fenceChar && length >= fenceLength && strings.TrimSpace(markdownFenceRemainder(line, marker)) == "" {
				inFence = false
			}
			continue
		}
		if inFence || isIndentedCodeLine(line) {
			continue
		}

		if reference, ok := parseReferenceDefinition(line, lineIndex+1); ok {
			references = append(references, reference)
		}
		references = append(references, parseInlineReferences(line, lineIndex+1)...)
	}
	return references
}

// markdownBodyStart skips the YAML frontmatter accepted by parseSkillDocument.
// It returns a line index so diagnostics retain their source locations.
func markdownBodyStart(lines []string) int {
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "---") {
		return 0
	}
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == "---" {
			return index + 1
		}
	}
	return 0
}

func parseInlineReferences(line string, lineNumber int) []markdownReference {
	references := make([]markdownReference, 0)
	for index := 0; index < len(line); index++ {
		if line[index] != '[' || isEscaped(line, index) {
			continue
		}
		closing := findClosingBracket(line, index)
		if closing < 0 {
			// No following balanced label can begin after an unmatched opening
			// bracket, so stop rather than rescanning the remaining suffix.
			return references
		}
		openParen := closing + 1
		for openParen < len(line) && (line[openParen] == ' ' || line[openParen] == '\t') {
			openParen++
		}
		if openParen >= len(line) || line[openParen] != '(' || isEscaped(line, openParen) {
			continue
		}
		destination, column, end, ok := parseInlineDestination(line, openParen)
		if !ok {
			continue
		}
		references = append(references, markdownReference{
			destination: destination,
			line:        lineNumber,
			column:      column,
		})
		index = end
	}
	return references
}

func parseInlineDestination(line string, openParen int) (string, int, int, bool) {
	index := openParen + 1
	for index < len(line) && (line[index] == ' ' || line[index] == '\t') {
		index++
	}
	if index >= len(line) || line[index] == ')' {
		return "", 0, 0, false
	}

	destinationStart := index
	if line[index] == '<' {
		index++
		for index < len(line) {
			if line[index] == '>' && !isEscaped(line, index) {
				destination := unescapeMarkdownDestination(line[destinationStart+1 : index])
				if destination == "" {
					return "", 0, 0, false
				}
				end, ok := inlineDestinationEnd(line, index+1)
				if !ok {
					return "", 0, 0, false
				}
				return destination, destinationStart + 2, end, true
			}
			index++
		}
		return "", 0, 0, false
	}

	depth := 0
	for index < len(line) {
		if isEscaped(line, index) {
			index++
			if index < len(line) {
				index++
			}
			continue
		}
		switch line[index] {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				destination := unescapeMarkdownDestination(line[destinationStart:index])
				if destination == "" {
					return "", 0, 0, false
				}
				return destination, destinationStart + 1, index, true
			}
			depth--
		case ' ', '\t':
			if depth == 0 {
				destination := unescapeMarkdownDestination(line[destinationStart:index])
				if destination == "" {
					return "", 0, 0, false
				}
				end, ok := inlineDestinationEnd(line, index)
				if !ok {
					return "", 0, 0, false
				}
				return destination, destinationStart + 1, end, true
			}
		}
		index++
	}
	return "", 0, 0, false
}

func inlineDestinationEnd(line string, index int) (int, bool) {
	for index < len(line) && (line[index] == ' ' || line[index] == '\t') {
		index++
	}
	if index >= len(line) {
		return 0, false
	}
	if line[index] == ')' && !isEscaped(line, index) {
		return index, true
	}
	if line[index] != '"' && line[index] != '\'' && line[index] != '(' {
		return 0, false
	}
	titleDelimiter := line[index]
	if titleDelimiter == '(' {
		return parenthesizedTitleEnd(line, index)
	}
	titleEnd := titleDelimiter
	index++
	for index < len(line) {
		if line[index] == titleEnd && !isEscaped(line, index) {
			index++
			for index < len(line) && (line[index] == ' ' || line[index] == '\t') {
				index++
			}
			if index < len(line) && line[index] == ')' && !isEscaped(line, index) {
				return index, true
			}
			return 0, false
		}
		index++
	}
	return 0, false
}

func parenthesizedTitleEnd(line string, index int) (int, bool) {
	depth := 1
	for index := index + 1; index < len(line); index++ {
		if isEscaped(line, index) {
			continue
		}
		switch line[index] {
		case '(':
			depth++
		case ')':
			depth--
			if depth != 0 {
				continue
			}
			index++
			for index < len(line) && (line[index] == ' ' || line[index] == '\t') {
				index++
			}
			if index < len(line) && line[index] == ')' && !isEscaped(line, index) {
				return index, true
			}
			return 0, false
		}
	}
	return 0, false
}

func parseReferenceDefinition(line string, lineNumber int) (markdownReference, bool) {
	index := 0
	for index < len(line) && line[index] == ' ' {
		index++
	}
	if index > 3 || index >= len(line) || line[index] != '[' || isEscaped(line, index) {
		return markdownReference{}, false
	}
	closing := findClosingBracket(line, index)
	if closing <= index || closing+1 >= len(line) || line[closing+1] != ':' {
		return markdownReference{}, false
	}
	index = closing + 2
	for index < len(line) && (line[index] == ' ' || line[index] == '\t') {
		index++
	}
	if index >= len(line) {
		return markdownReference{}, false
	}
	destinationStart := index
	if line[index] == '<' {
		index++
		for index < len(line) {
			if line[index] == '>' && !isEscaped(line, index) {
				destination := unescapeMarkdownDestination(line[destinationStart+1 : index])
				if destination == "" {
					return markdownReference{}, false
				}
				return markdownReference{destination: destination, line: lineNumber, column: destinationStart + 2}, true
			}
			index++
		}
		return markdownReference{}, false
	}
	for index < len(line) && line[index] != ' ' && line[index] != '\t' {
		if line[index] == '\\' && index+1 < len(line) {
			index += 2
			continue
		}
		index++
	}
	destination := unescapeMarkdownDestination(line[destinationStart:index])
	if destination == "" {
		return markdownReference{}, false
	}
	return markdownReference{destination: destination, line: lineNumber, column: destinationStart + 1}, true
}

func findClosingBracket(line string, opening int) int {
	depth := 0
	for index := opening; index < len(line); index++ {
		if isEscaped(line, index) {
			continue
		}
		switch line[index] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func maskInlineCode(line string) string {
	masked := []byte(line)
	openers := map[int]int{}
	for index := 0; index < len(line); {
		if line[index] != '`' || isEscaped(line, index) {
			index++
			continue
		}
		runLength := 1
		for index+runLength < len(line) && line[index+runLength] == '`' {
			runLength++
		}
		if opening, ok := openers[runLength]; ok {
			for position := opening; position < index+runLength; position++ {
				if masked[position] != '\n' && masked[position] != '\r' {
					masked[position] = ' '
				}
			}
			delete(openers, runLength)
		} else {
			openers[runLength] = index
		}
		index += runLength
	}
	return string(masked)
}

func markdownFence(line string) (byte, int, bool) {
	index := 0
	for index < len(line) && line[index] == ' ' {
		index++
	}
	if index > 3 || index >= len(line) || (line[index] != '`' && line[index] != '~') {
		return 0, 0, false
	}
	marker := line[index]
	length := 0
	for index+length < len(line) && line[index+length] == marker {
		length++
	}
	if length < 3 {
		return 0, 0, false
	}
	return marker, length, true
}

func markdownFenceRemainder(line string, marker byte) string {
	index := strings.IndexByte(line, marker)
	if index < 0 {
		return line
	}
	for index < len(line) && line[index] == marker {
		index++
	}
	return line[index:]
}

func isIndentedCodeLine(line string) bool {
	spaces := 0
	for spaces < len(line) && line[spaces] == ' ' {
		spaces++
	}
	return spaces >= 4
}

func isEscaped(line string, index int) bool {
	backslashes := 0
	for index > 0 && line[index-1] == '\\' {
		backslashes++
		index--
	}
	return backslashes%2 == 1
}

func unescapeMarkdownDestination(destination string) string {
	var builder strings.Builder
	for index := 0; index < len(destination); index++ {
		if destination[index] == '\\' && index+1 < len(destination) && markdownEscapable(destination[index+1]) {
			builder.WriteByte(destination[index+1])
			index++
			continue
		}
		builder.WriteByte(destination[index])
	}
	return builder.String()
}

func markdownEscapable(char byte) bool {
	if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
		return false
	}
	return strings.ContainsRune(`!"#$%&'()*+,-./:;<=>?@[\]^_`+"`"+`{|}~`, rune(char)) || char == ' '
}
