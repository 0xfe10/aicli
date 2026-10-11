//go:build windows

package paseobridgert

import "os"

// Windows ownership is managed by filesystem ACLs, not Unix uid/mode bits.
func ownedByCurrentUser(info os.FileInfo) bool { return true }
