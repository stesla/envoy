package gotelnet

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
