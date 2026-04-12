package main

import (
	"fmt"
	"os"
)

type ExitCode int

const (
	ExitSuccess  ExitCode = 0
	ExitError    ExitCode = 1
	ExitUsage    ExitCode = 2
	ExitNotFound ExitCode = 3
)

type MnemosError struct {
	Code    ExitCode
	Message string
	Cause   error
}

func (e *MnemosError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *MnemosError) Unwrap() error {
	return e.Cause
}

func NewUserError(format string, args ...any) *MnemosError {
	return &MnemosError{Code: ExitUsage, Message: fmt.Sprintf(format, args...)}
}

func NewNotFoundError(format string, args ...any) *MnemosError {
	return &MnemosError{Code: ExitNotFound, Message: fmt.Sprintf(format, args...)}
}

func NewSystemError(cause error, format string, args ...any) *MnemosError {
	return &MnemosError{Code: ExitError, Message: fmt.Sprintf(format, args...), Cause: cause}
}

func WrapError(code ExitCode, format string, cause error) *MnemosError {
	return &MnemosError{Code: code, Message: format, Cause: cause}
}

var _ error = (*MnemosError)(nil)

func exitWithMnemosError(verbose bool, err error) {
	if err == nil {
		os.Exit(int(ExitSuccess))
	}

	code := ExitError
	msg := err.Error()

	if me, ok := err.(*MnemosError); ok {
		code = me.Code
		if verbose || code == ExitUsage {
			msg = me.Error()
		} else {
			msg = me.Message
		}
	}

	fmt.Fprintf(os.Stderr, "error: %s\n", msg)
	os.Exit(int(code))
}
