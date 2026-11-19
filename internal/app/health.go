package app

import "context"

// EnsureSupervisor is installed by the CLI. Remote clients call it after a
// transport failure so a crashed supervisor can be recreated without treating a
// user stop as a crash.
var EnsureSupervisor func(context.Context) error

// Health reports whether the frontend is usable. The supervisor's own
// implementation is always healthy; remote clients override this via IPC.
func (a *App) Health(context.Context) error { return nil }
