// Package xerr holds sentinel errors mapped to process exit codes.
package xerr

import "errors"

var (
	// ErrAmbiguous means more than one release asset matched and none won.
	ErrAmbiguous = errors.New("ambiguous")
	// ErrNotFound means a repo, release, extension, or asset does not exist.
	ErrNotFound = errors.New("not found")
	// ErrVerify means a download failed size, digest, or archive checks.
	ErrVerify = errors.New("verification failed")
)

// ExitCode maps an error to a ghx process status.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, ErrAmbiguous):
		return 2
	case errors.Is(err, ErrNotFound):
		return 3
	case errors.Is(err, ErrVerify):
		return 4
	default:
		return 1
	}
}
