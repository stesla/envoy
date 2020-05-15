package proxy

import (
	"bytes"
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
	var buf bytes.Buffer
	h := &history{buf: []byte("foo\nbar\nbaz")}
	nw, ew := h.WriteTo(&buf)
	assert.NoError(t, ew)
	assert.Equal(t, int64(7), nw)
	assert.Equal(t, "bar\nbaz", buf.String())
}

func TestConnectString(t *testing.T) {
	tests := []struct {
		onconnect, name, password string
		expected                  string
	}{
		{onconnect: "connect foo bar", name: "baz", password: "quux", expected: "connect foo bar"},
		{name: "foo", password: "xyzzy", expected: "connect \"foo\" xyzzy"},
	}
	for _, test := range tests {
		p := &proxy{Name: test.name, Password: test.password, OnConnect: test.onconnect}
		assert.Equal(t, test.expected, p.ConnectString())
	}
}
