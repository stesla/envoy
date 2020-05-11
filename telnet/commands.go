package telnet

import "fmt"

const (
	// RFC 885
	EOR = 239 + iota
	// RFC 854
	SE
	NOP
	DM
	BRK
	IP
	AO
	AYT
	EC
	EL
	GA
	SB
	WILL
	WONT
	DO
	DONT
	IAC
)

type commandByte byte

func (c commandByte) String() string {
	str, ok := map[commandByte]string{
		AO:   "AO",
		AYT:  "AYT",
		SB:   "SB",
		BRK:  "BRK",
		DM:   "DM",
		DO:   "DO",
		DONT: "DONT",
		Echo: "ECHO",
		SE:   "SE",
		EC:   "EC",
		EL:   "EL",
		GA:   "GA",
		IAC:  "IAC",
		IP:   "IP",
		NOP:  "NOP",
		WILL: "WILL",
		WONT: "WONT",
	}[c]
	if ok {
		return str
	}
	return fmt.Sprintf("%d", c)
}

const (
	TransmitBinary  = 0  // RFC 856
	Echo            = 1  // RFC 857
	SuppressGoAhead = 3  // RFC 858
	Charset         = 42 // RFC 2066
	TerminalType    = 24 // RFC 930
	NAWS            = 31 // RFC 1073
	EndOfRecord     = 25 // RFC 885
)

type optionByte byte

func (c optionByte) String() string {
	str, ok := map[optionByte]string{
		Charset:         "CHARSET",
		EndOfRecord:     "END-OF-RECORD",
		NAWS:            "NAWS",
		SuppressGoAhead: "SUPPRESS-GO-AHEAD",
		TerminalType:    "TERMINAL-TYPE",
		TransmitBinary:  "TRANSMIT-BINARY",
	}[c]
	if ok {
		return str
	}
	return fmt.Sprintf("%d", c)
}

type charsetByte byte

const (
	charsetRequest = 1 + iota
	charsetAccepted
	charsetRejected
	charsetTTableIs
	charsetTTableRejected
	charsetTTableAck
	charsetTTableNak
)

func (c charsetByte) String() string {
	str, ok := map[charsetByte]string{
		charsetRequest:        "REQUEST",
		charsetAccepted:       "ACCEPTED",
		charsetRejected:       "REJECTED",
		charsetTTableIs:       "TTABLE-IS",
		charsetTTableRejected: "TTABLE-REJECTED",
		charsetTTableAck:      "TTABLE-ACK",
		charsetTTableNak:      "TTABLE-NAK",
	}[c]
	if ok {
		return str
	}
	return fmt.Sprintf("%d", c)
}
