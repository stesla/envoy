package telnet

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestClientReadAscii(t *testing.T) {
	var expected = []byte("abc123")
	var r = bytes.NewReader(expected)
	c := NewClient(r, nil)

	var buf = make([]byte, 2*r.Len())
	nr, er := c.Read(buf)

	assert.NoError(t, er)
	assert.Equal(t, len(expected), nr)
	assert.Equal(t, expected, buf[:nr])
}

func TestClientReadNonAscii(t *testing.T) {
	var r = bytes.NewReader([]byte("a»b"))
	c := NewClient(r, nil)

	var buf = make([]byte, 2*r.Len())
	nr, er := c.Read(buf)

	var expected = []byte("a?b")
	assert.NoError(t, er)
	assert.Equal(t, len(expected), nr)
	assert.Equal(t, expected, buf[:nr])
}

func TestClientWriteAscii(t *testing.T) {
	var w bytes.Buffer
	c := NewClient(nil, &w)

	var expected = []byte("abc123")
	nw, ew := c.Write(expected)

	if assert.NoError(t, ew) {
		assert.Equal(t, len(expected), nw)
		assert.Equal(t, expected, w.Bytes())
	}
}

func TestClientWriteNonAscii(t *testing.T) {
	var w bytes.Buffer
	c := NewClient(nil, &w)

	nw, ew := c.Write([]byte("abc»123"))
	if assert.Error(t, ew) {
		assert.Equal(t, 3, nw)
		assert.Equal(t, "abc", w.String())
	}
}
