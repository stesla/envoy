package telnet

import "fmt"

const (
	TransmitBinary  = 0   // RFC 856
	Echo            = 1   // RFC 857
	SuppressGoAhead = 3   // RFC 858
	Charset         = 42  // RFC 2066
	TerminalType    = 24  // RFC 930
	NAWS            = 31  // RFC 1073
	EndOfRecord     = 25  // RFC 885
	EOR             = 239 // RFC 885
)

const (
	// RFC 854
	SE = 240 + iota
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

type command byte

func (c command) String() string {
	switch c {
	case AO:
		return "AO"
	case AYT:
		return "AYT"
	case SB:
		return "SB"
	case BRK:
		return "BRK"
	case Charset:
		return "CHARSET"
	case DM:
		return "DM"
	case DO:
		return "DO"
	case DONT:
		return "DONT"
	case Echo:
		return "ECHO"
	case EndOfRecord:
		return "END-OF-RECORD"
	case SE:
		return "SE"
	case EC:
		return "EC"
	case EL:
		return "EL"
	case GA:
		return "GA"
	case IAC:
		return "IAC"
	case IP:
		return "IP"
	case NAWS:
		return "NAWS"
	case NOP:
		return "NOP"
	case SuppressGoAhead:
		return "SUPPRESS-GO-AHEAD"
	case TerminalType:
		return "TERMINAL-TYPE"
	case WILL:
		return "WILL"
	case WONT:
		return "WONT"
	case TransmitBinary:
		return "TRANSMIT-BINARY"
	default:
		return fmt.Sprintf("%d", c)
	}
}
