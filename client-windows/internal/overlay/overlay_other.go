//go:build !windows

package overlay

type SessionOverlay struct{}

func NewSessionOverlay() *SessionOverlay {
	return &SessionOverlay{}
}

func (so *SessionOverlay) NotifyActivity() {}

func (so *SessionOverlay) Close() {}
