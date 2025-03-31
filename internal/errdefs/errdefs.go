package errdefs

import (
	"errors"
	"fmt"
)

type ErrorType int

// Errors
var (
	ErrNotRoot                     = errors.New("This installer must be run with root privileges")
	ErrK3sInstallFailed            = NewCustomError(ErrTypeK3sInstallFailed, "")
	ErrNotLinux                    = NewCustomError(ErrTypeNotLinux, "")
	ErrDistributionDetectionFailed = NewCustomError(ErrTypeDistributionDetectionFailed, "")
	ErrUnsupportedDistribution     = NewCustomError(ErrTypeUnsupportedDistribution, "")
	ErrUnsupportedVersion          = NewCustomError(ErrTypeUnsupportedVersion, "")
)

// More dynamic errors
const (
	ErrTypeNotLinux ErrorType = iota
	ErrTypeDistributionDetectionFailed
	ErrTypeUnsupportedDistribution
	ErrTypeUnsupportedVersion
	ErrTypeK3sInstallFailed
)

var errorTypeStrings = map[ErrorType]string{
	ErrTypeNotLinux:                    "ErrNotLinux",
	ErrTypeDistributionDetectionFailed: "ErrDistributionDetectionFailed",
	ErrTypeUnsupportedDistribution:     "ErrUnsupportedDistribution",
	ErrTypeUnsupportedVersion:          "ErrUnsupportedVersion",
	ErrTypeK3sInstallFailed:            "ErrK3sInstallFailed",
}

func (e ErrorType) String() string {
	if s, ok := errorTypeStrings[e]; ok {
		return s
	}
	return "ErrUnknown"
}

type CustomError struct {
	Type    ErrorType
	Message string
}

func (e *CustomError) Error() string {
	return fmt.Sprintf("%s: %s", e.Type.String(), e.Message)
}

func (e *CustomError) Is(target error) bool {
	t, ok := target.(*CustomError)
	if !ok {
		return false
	}
	return e.Type == t.Type
}

func NewCustomError(t ErrorType, message string) *CustomError {
	return &CustomError{
		Type:    t,
		Message: message,
	}
}
