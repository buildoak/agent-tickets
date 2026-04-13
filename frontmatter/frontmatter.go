// Package frontmatter provides byte-exact round-trip parsing and mutation
// of markdown files with YAML frontmatter.
//
// Design: The file is split at --- delimiters. The YAML header is parsed
// into a Card struct. The body (everything after the closing ---) is kept
// as raw bytes and never touched. On write, the updated YAML header is
// serialized and concatenated with the original raw body bytes.
package frontmatter
