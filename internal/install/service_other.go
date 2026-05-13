//go:build !linux && !darwin

package install

import "errors"

var errServiceNotSupported = errors.New("service registration not supported on this platform")

func platformWriteService(_ string) (string, error) {
	return "", errServiceNotSupported
}

func platformEnableService() error {
	return errServiceNotSupported
}
