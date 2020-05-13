package proxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHistoryScrolls(t *testing.T) {
	h := newHistoryWithSize(8, 2)
	tests := []struct {
		input, expected string
	}{
		// order matters here
		{"", ""},
		{"abcdefgh", "abcdefgh"},
		{"i", "cdefghi"},
		{"jklm", "ghijklm"},
		{"nopqrstuvwxyz", "stuvwxyz"},
		{"abcdefghijklmnopqrstuvwxyz1", "uvwxyz1"},
	}
	for _, test := range tests {
		n, err := h.Write([]byte(test.input))
		assert.NoError(t, err)
		assert.Equal(t, n, len(test.input))
		assert.Equal(t, test.expected, string(h.buf))
	}
}

func TestRemovesFirstLine(t *testing.T) {
	// We are scrolling the history by bytes, so it's very likely that the
	// first line in the buffer is actually a partial line. Trim it off.
	// But only trim it off if the buffer got scrolled.

	// order matetrs here
	h := newHistoryWithSize(10, 5)
	h.Write([]byte("foo\nbar"))
	assert.Equal(t, "foo\nbar", string(h.buf))

	h.Write([]byte("\nquux\nxyzzy"))
	assert.Equal(t, "xyzzy", string(h.buf))

}
