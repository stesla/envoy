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

var testLogFields = log.Fields{"type": ServerType}

/*
 * Raw Log
 */

func TestRawLog(t *testing.T) {
	var outr bytes.Buffer
	var outw bytes.Buffer
	var raw bytes.Buffer
	expected := []byte{'h', IAC, DO, Echo, 'i'}
	r := bytes.NewReader(expected)

	c := newConnection(testLogFields, r, &outw)
	c.SetRawLogWriter(&raw)
	outr.ReadFrom(c)

	assert.Equal(t, "hi", outr.String())
	assert.Equal(t, expected, raw.Bytes())
}
