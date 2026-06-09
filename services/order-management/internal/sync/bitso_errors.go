package sync

import (
	"errors"

	"bitso-trading-platform/shared/pkg/bitso"
)

func bitsoErrorCode(err error) int {
	if err == nil {
		return 0
	}
	var berr *bitso.Error
	if errors.As(err, &berr) {
		return berr.Code()
	}
	return 0
}

func isBitsoErrorCode(err error, code int) bool {
	return bitsoErrorCode(err) == code
}
