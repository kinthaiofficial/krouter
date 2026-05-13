//go:build !linux && !darwin

package uninstall

func platformStopService() error    { return nil }
func platformRemoveServiceFile() error { return nil }
