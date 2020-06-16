package telnet

import (
	"golang.org/x/text/encoding"
	"golang.org/x/text/transform"
)

var ASCII encoding.Encoding = &asciiEncoding{}

type asciiEncoding struct{}

func (a asciiEncoding) NewDecoder() *encoding.Decoder {
	return &encoding.Decoder{Transformer: a}
}

func (a asciiEncoding) NewEncoder() *encoding.Encoder {
	return &encoding.Encoder{Transformer: a}
}

func (asciiEncoding) String() string { return "ASCII" }

func (a asciiEncoding) Transform(dst, src []byte, atEOF bool) (nDst, nSrc int, err error) {
	for i, c := range src {
		if nDst >= len(dst) {
			err = transform.ErrShortDst
			break
		}
		if c < 128 {
			dst[nDst] = c
		} else {
			dst[nDst] = '\x1A'
		}
		nDst++
		nSrc = i + 1
	}
	return
}

func (a asciiEncoding) Reset() {}
