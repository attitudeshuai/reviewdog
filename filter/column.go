package filter

import (
	"unicode/utf8"

	"github.com/reviewdog/reviewdog/proto/rdf"
)

// Columns of rdf.Position are 1-based UTF-8 byte offsets. The helpers below
// turn them into 0-based byte offsets which never point past a line end and
// never land in the middle of a multi-byte rune, so any text sliced with
// them stays valid UTF-8.

// StartByteOffset maps a 1-based start column to a 0-based byte offset on the
// given line. An omitted (zero or negative) column means the start of the
// line. An offset past the line end is clamped to the line end, and an offset
// in the middle of a multi-byte rune expands outward to the beginning of that
// rune.
func StartByteOffset(line string, column int) int {
	if column <= 0 {
		return 0
	}
	off := column - 1
	if off >= len(line) {
		return len(line)
	}
	for off > 0 && !utf8.RuneStart(line[off]) {
		off--
	}
	return off
}

// EndByteOffset maps a 1-based exclusive end column to a 0-based byte offset
// on the given line. An omitted (zero or negative) column means the end of
// the line. An offset past the line end is clamped to the line end, and an
// offset in the middle of a multi-byte rune expands outward to the end of
// that rune.
func EndByteOffset(line string, column int) int {
	if column <= 0 {
		return len(line)
	}
	off := column - 1
	if off >= len(line) {
		return len(line)
	}
	for off < len(line) && !utf8.RuneStart(line[off]) {
		off++
	}
	return off
}

// NormalizeLocationColumns clamps the columns of the diagnostic location
// range to the available source lines, so the same corrected columns reach
// every reporter (local summary, structured output, check-run annotations).
//
// Columns are only touched when the corresponding source line content is
// available; ranges without source content keep their original values. The
// range shape itself is never changed: omitted columns stay omitted.
func NormalizeLocationColumns(loc *rdf.Location, sourceLines map[int]string) {
	if loc == nil || len(sourceLines) == 0 {
		return
	}
	rng := loc.GetRange()
	if rng == nil {
		return
	}
	if start := rng.GetStart(); start != nil && start.GetColumn() > 0 {
		if line, ok := sourceLines[int(start.GetLine())]; ok {
			start.Column = int32(StartByteOffset(line, int(start.GetColumn()))) + 1
		}
	}
	if end := rng.GetEnd(); end != nil && end.GetColumn() > 0 {
		// An end position without a line defaults to the start line.
		endLine := int(end.GetLine())
		if endLine == 0 {
			endLine = int(rng.GetStart().GetLine())
		}
		if line, ok := sourceLines[endLine]; ok {
			end.Column = int32(EndByteOffset(line, int(end.GetColumn()))) + 1
		}
	}
}
