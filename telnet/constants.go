package telnet

// Core Telnet Constants
const (
	// RFC 885
	EndOfRecord = iota + 239
	// RFC 854
	EndSubnegotiation
	NoOperation
	DataMark
	Break
	InterruptProcess
	AbortOutput
	AreYouThere
	EraseCharacter
	EraseLine
	GoAhead
	BeginSubnegotiation
	Will
	Wont
	Do
	Dont
	InterpretAsCommand
)

// Telnet Options
const (
	TransmitBinary  = 0  // RFC 856
	SuppressGoAhead = 3  // RFC 858
	Charset         = 42 // RFC 2066
	TerminalType    = 24 // RFC 930
	NAWS            = 31 // RFC 1073
)
