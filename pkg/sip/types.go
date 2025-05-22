package sip

// Define any SIP-specific types here

// Dialog states for SIP dialogs
type DialogState int

const (
	// DialogNone represents no dialog or initial state
	DialogNone DialogState = iota

	// DialogEarly represents early dialog state
	DialogEarly

	// DialogConfirmed represents confirmed dialog state
	DialogConfirmed

	// DialogTerminated represents terminated dialog state
	DialogTerminated
)

// String returns the string representation of a dialog state
func (s DialogState) String() string {
	switch s {
	case DialogNone:
		return "none"
	case DialogEarly:
		return "early"
	case DialogConfirmed:
		return "confirmed"
	case DialogTerminated:
		return "terminated"
	default:
		return "unknown"
	}
}
