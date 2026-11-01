package desktop

import "github.com/Stilltiger45/TigerSB-client/mobile"

// Engine is the core as the service drives it: the mobile package, or a
// fake in tests.
type Engine interface {
	StartTUN(name, mode string, mtu int) error
	Stop() error
	Running() bool
	// Healthy checks the channel through the server in use.
	Healthy() bool
	// Online checks the computer's own network, outside the VPN.
	Online() bool
	// Freezing tells the user what helps when fresh connections work and
	// old ones freeze, "" when they do not.
	Freezing() string
	// TakeRestart reports, once, that the core fetched the routing lists
	// it started without and wants a restart.
	TakeRestart() bool
	ResetConnections() int
	SwitchNetwork(kind string) int
	PickMTU(kind string, linkMTU int) int
	PathKnown() bool
	SetNetwork(kind string)
	Note(event string)
}

// Core is the real core.
type Core struct{}

func (Core) StartTUN(name, mode string, mtu int) error { return mobile.StartTUN(name, mode, mtu) }
func (Core) Stop() error                               { return mobile.Stop() }
func (Core) Running() bool                             { return mobile.Running() }
func (Core) Healthy() bool                             { return mobile.Healthy() }
func (Core) Online() bool                              { return mobile.Online() }
func (Core) Freezing() string                          { return mobile.Freezing() }
func (Core) TakeRestart() bool                         { return mobile.TakeRestart() }
func (Core) ResetConnections() int                     { return mobile.ResetConnections() }
func (Core) SwitchNetwork(kind string) int             { return mobile.SwitchNetwork(kind) }
func (Core) PickMTU(kind string, linkMTU int) int      { return mobile.PickMTU(kind, linkMTU) }
func (Core) PathKnown() bool                           { return mobile.PathKnown() }
func (Core) SetNetwork(kind string)                    { mobile.SetNetwork(kind) }
func (Core) Note(event string)                         { mobile.Note(event) }
