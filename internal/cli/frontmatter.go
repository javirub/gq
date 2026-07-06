package cli

import "bytes"

// splitFrontMatter splits a document into its yaml front matter and the
// remaining content, mirroring yqlib's frontMatterHandler.Split (which gq
// avoids because it leaks the original file handle, locking the file on
// Windows). The front matter includes the leading "---" line; the rest
// starts at the second "---" marker.
func splitFrontMatter(content []byte) (frontMatter, rest []byte) {
	offset := 0
	lineCount := 0
	for offset < len(content) {
		if lineCount > 0 && bytes.HasPrefix(content[offset:], []byte("---")) {
			break
		}
		lineEnd := bytes.IndexByte(content[offset:], '\n')
		if lineEnd < 0 {
			offset = len(content)
			break
		}
		offset += lineEnd + 1
		lineCount++
	}
	return content[:offset], content[offset:]
}
