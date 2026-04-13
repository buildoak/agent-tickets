package frontmatter

import "strings"

func (d *Document) GetSection(name string) string {
	start, contentStart, end, ok := findSectionBounds(string(d.Body), name)
	_ = start
	if !ok {
		return ""
	}

	return string(d.Body[contentStart:end])
}

func (d *Document) SetSection(name string, content string) {
	body := string(d.Body)
	_, contentStart, end, ok := findSectionBounds(body, name)
	if ok {
		updated := body[:contentStart] + content + body[end:]
		d.Body = []byte(updated)
		return
	}

	if len(body) > 0 && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if len(body) > 0 && !strings.HasSuffix(body, "\n\n") {
		body += "\n"
	}

	body += "## " + name + "\n"
	body += content
	d.Body = []byte(body)
}

func (d *Document) AppendToSection(name string, text string) {
	current := d.GetSection(name)
	d.SetSection(name, current+text)
}

func findSectionBounds(body string, name string) (sectionStart int, contentStart int, sectionEnd int, ok bool) {
	header := "## " + name
	lineStarts := []int{0}
	for i := 0; i < len(body); i++ {
		if body[i] == '\n' && i+1 < len(body) {
			lineStarts = append(lineStarts, i+1)
		}
	}

	for _, start := range lineStarts {
		lineEnd := strings.IndexByte(body[start:], '\n')
		end := len(body)
		if lineEnd != -1 {
			end = start + lineEnd
		}
		line := strings.TrimSuffix(body[start:end], "\r")
		if line != header {
			continue
		}

		sectionStart = start
		if lineEnd == -1 {
			contentStart = len(body)
			return sectionStart, contentStart, len(body), true
		}
		contentStart = end + 1
		sectionEnd = len(body)

		for _, nextStart := range lineStarts {
			if nextStart <= start {
				continue
			}
			nextEnd := len(body)
			nextLineEnd := strings.IndexByte(body[nextStart:], '\n')
			if nextLineEnd != -1 {
				nextEnd = nextStart + nextLineEnd
			}
			nextLine := strings.TrimSuffix(body[nextStart:nextEnd], "\r")
			if strings.HasPrefix(nextLine, "## ") {
				sectionEnd = nextStart
				break
			}
		}
		return sectionStart, contentStart, sectionEnd, true
	}

	return 0, 0, 0, false
}
