package telnet

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

/*
 * Raw Log
 */

func TestRawLog(t *testing.T) {
	var outr bytes.Buffer
	var outw bytes.Buffer
	var raw bytes.Buffer
	expected := []byte{'h', IAC, DO, Echo, 'i'}
	r := bytes.NewReader(expected)

	c := newConnection(ServerType, r, &outw)
	c.SetRawLogWriter(&raw)
	outr.ReadFrom(c)

	assert.Equal(t, "hi", outr.String())
	assert.Equal(t, expected, raw.Bytes())
}
