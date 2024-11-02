package utils

import "runtime"

func CheckOSType() string {
	return runtime.GOOS
}
