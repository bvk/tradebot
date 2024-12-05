//go:build !windows

package sglog

import "os/user"

func lookupUser() string {
	if current, err := user.Current(); err == nil {
		return current.Username
	}
	return ""
}
