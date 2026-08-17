// Package pdf contains deliberately small, dependency-free PDF helpers used
// by the API when a snapshot is archived.  The browser still uses PDF.js for
// rendering; these helpers only need enough information to bind a snapshot to
// stable page metadata without shelling out to a host PDF utility.
package pdf

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
)

// Metadata is the immutable PDF metadata stored with a snapshot.
type Metadata struct {
	PageCount int
	// PageTextHashes is named for compatibility with the existing schema.  A
	// lightweight parser cannot faithfully reproduce every PDF text layout,
	// so hashes are based on each page object's byte segment.  This remains a
	// useful deterministic fallback for page-level comparisons and malformed
	// but header-valid files can still be rejected by the browser PDF viewer.
	PageTextHashes []string
}

var pageObjectPattern = regexp.MustCompile(`/Type\s*/Page(?:\s|[/ >])`)

// Analyze validates the PDF signature and derives deterministic page hashes.
// It intentionally does not attempt to execute or interpret PDF content.
func Analyze(data []byte) (Metadata, error) {
	if len(data) < 5 || !bytes.HasPrefix(data, []byte("%PDF-")) {
		return Metadata{}, errors.New("invalid PDF signature")
	}
	// A header alone is not sufficient to identify a readable PDF.  We do not
	// attempt to implement a complete PDF parser here, but every PDF written by
	// the supported producers terminates with %%EOF (possibly followed by
	// whitespace).  Rejecting truncated uploads prevents them from becoming
	// apparently valid snapshots that PDF.js cannot load later.
	eof := bytes.LastIndex(data, []byte("%%EOF"))
	if eof < 0 || len(bytes.TrimSpace(data[eof+len("%%EOF"):])) != 0 {
		return Metadata{}, errors.New("truncated PDF")
	}

	locations := pageObjectPattern.FindAllIndex(data, -1)
	if len(locations) == 0 {
		// Keep compatibility with old projects that stored a header-valid PDF
		// before page metadata was available.  The PDF viewer remains the final
		// authority on whether such a file can be rendered.
		return Metadata{PageCount: 1, PageTextHashes: []string{hashBytes(data)}}, nil
	}

	hashes := make([]string, 0, len(locations))
	for index, location := range locations {
		start := location[0]
		end := len(data)
		if index+1 < len(locations) {
			end = locations[index+1][0]
		}
		hashes = append(hashes, hashBytes(data[start:end]))
	}
	return Metadata{PageCount: len(hashes), PageTextHashes: hashes}, nil
}

func hashBytes(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
