package frontmatter

type Document struct {
	Card Card
	Body []byte

	rawHeader     []byte
	originalCard  Card
	fieldOrder    []string
	rawFieldBytes map[string][]byte
}
