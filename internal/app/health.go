package app

import "context"

// Health reports whether the frontend is usable. The supervisor's own
// implementation is always healthy; remote clients override this via IPC.
func (a *App) Health(context.Context) error { return nil }
