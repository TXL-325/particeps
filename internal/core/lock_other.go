//go:build !linux

package core

import (
	"fmt"
	"os"
)

func lockDataDirectory(string) (*os.File, error) {
	return nil, fmt.Errorf("the Particeps Agent requires Linux")
}
