//go:build !windows

package tray

// Tray is a no-op outside Windows. All methods are safe on a nil *Tray.
type Tray struct{}

func Start() *Tray                   { return nil }
func (t *Tray) Events() <-chan Event { return nil }
func (t *Tray) Update(State)         {}
func (t *Tray) Stop()                {}
