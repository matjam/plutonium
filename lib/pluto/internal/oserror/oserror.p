// Package oserror defines the portable file system errors that pluto/os
// exports and that syscall.Errno matches through its Is method. It exists so
// that syscall need not import os.
package oserror

import "pluto/errors"

var (
	ErrInvalid    = errors.New("invalid argument")
	ErrPermission = errors.New("permission denied")
	ErrExist      = errors.New("file already exists")
	ErrNotExist   = errors.New("file does not exist")
	ErrClosed     = errors.New("file already closed")
)
