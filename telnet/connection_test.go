package telnet

import (
	"bytes"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

/*
 * Read
 */

var testLogFields = log.Fields{"type": ClientType}

func TestConnReadAscii(t *testing.T) {
	var expected = []byte("abc123")
	var r = bytes.NewReader(expected)
	c := newConnection(testLogFields, r, nil)

	var buf = make([]byte, 2*r.Len())
	nr, er := c.Read(buf)

	assert.NoError(t, er)
	assert.Equal(t, len(expected), nr)
	assert.Equal(t, expected, buf[:nr])
}

func TestConnReadNonAscii(t *testing.T) {
	var r = bytes.NewReader([]byte("a\x80b"))
	c := newConnection(testLogFields, r, nil)

	var buf = make([]byte, 2*r.Len())
	nr, er := c.Read(buf)

	var expected = []byte("a\x1ab")
	assert.NoError(t, er)
	assert.Equal(t, len(expected), nr)
	assert.Equal(t, expected, buf[:nr])
}

/*
 * Write()
 */

func TestConnWriteAscii(t *testing.T) {
	var w bytes.Buffer
	c := newConnection(testLogFields, nil, &w)

	var expected = []byte("abc123")
	nw, ew := c.Write(expected)

	if assert.NoError(t, ew) {
		assert.Equal(t, len(expected), nw)
		assert.Equal(t, expected, w.Bytes())
	}
}

func TestConnWriteNonAscii(t *testing.T) {
	var w bytes.Buffer
	c := newConnection(testLogFields, nil, &w)

	nw, ew := c.Write([]byte("abc\x80123"))
	assert.NoError(t, ew)
	assert.Equal(t, 7, nw)
	assert.Equal(t, "abc\x1A123", w.String())
}
