package awg

// SetVerbose sends the devices' detailed log to f.
func SetVerbose(f func(format string, args ...any)) { verbosef = f }
