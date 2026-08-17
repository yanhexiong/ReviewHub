package pdf

import "testing"

func TestAnalyzeRejectsTruncatedPDF(t *testing.T) {
	if _, err := Analyze([]byte("%PDF-1.7\n1 0 obj << /Type /Page >>")); err == nil {
		t.Fatal("truncated PDF was accepted")
	}
}

func TestAnalyzeAcceptsMinimalPDFWithEOF(t *testing.T) {
	metadata, err := Analyze([]byte("%PDF-1.7\n1 0 obj << /Type /Page >>\n%%EOF\n"))
	if err != nil {
		t.Fatalf("minimal PDF rejected: %v", err)
	}
	if metadata.PageCount != 1 || len(metadata.PageTextHashes) != 1 {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
}
