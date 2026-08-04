package domain

// Social handles.
//
// A person knows they are @something on Instagram. They do not know which of
// the several shapes of Facebook URL is theirs, and asking for a URL is how a
// profile ends up pointing at facebook.com/notifications/.
//
// What is checked here is deliberately not what Facebook and Instagram allow.
// Both change their username rules and their reserved names, and a handle that
// passes any rule may still belong to nobody. So this checks only that the
// value is a single safe path segment, and nothing in the app claims the
// account exists or belongs to whoever typed it.

import (
	"errors"
	"strings"
)

// ErrBadHandle is returned for anything that cannot be part of a profile URL.
var ErrBadHandle = errors.New("that is not a username")

const maxHandle = 60

// NormaliseHandle cleans up what somebody typed and returns the handle, or the
// empty string to mean "no link". Anything else is an error rather than a
// silent correction: quietly turning a pasted URL into its last path segment is
// how a share link becomes somebody else's name.
func NormaliseHandle(in string) (string, error) {
	// A leading @ is how the handle is written everywhere else, so it is
	// expected rather than wrong.
	h := strings.TrimPrefix(strings.TrimSpace(in), "@")
	if h == "" {
		return "", nil
	}
	if len(h) > maxHandle {
		return "", ErrBadHandle
	}
	for _, r := range h {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			// Slashes, spaces, ?, #, : and everything else. A value carrying
			// any of them is a URL or a mistake, and both need the person to
			// look again rather than have it guessed at.
			return "", ErrBadHandle
		}
	}
	return h, nil
}

// FacebookURL and InstagramURL compose the link from a fixed origin and a
// handle that has already been checked, so nothing a user typed decides where
// the link points.
func FacebookURL(handle string) string {
	if handle == "" {
		return ""
	}
	return "https://facebook.com/" + handle
}

func InstagramURL(handle string) string {
	if handle == "" {
		return ""
	}
	return "https://instagram.com/" + handle
}
