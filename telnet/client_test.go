package telnet

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestReadASCII(t *testing.T) {
	var expected = []byte("abc123")
	var r = bytes.NewReader(expected)
	var w bytes.Buffer
	c := NewClient(r, &w)

	var buf = make([]byte, 2*r.Len())
	nr, er := c.Read(buf)

	assert.NoError(t, er)
	assert.Equal(t, len(expected), nr)
	assert.Equal(t, expected, buf[:nr])
}
