package utils

import (
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// RandomString returns a string of random alpha characters of the specified
// length.
func RandomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, n)
	lenLetters := len(letters)
	// The global random source is not used so that we do not generate the same
	// strings upon restart of the stack. Also, we do not have a shared source
	// for the package since a local source is not thread-safe.
	rng := rand.New(rand.NewSource(time.Now().UTC().UnixNano()))
	for i := 0; i < n; i++ {
		b[i] = letters[rng.Intn(lenLetters)]
	}
	return string(b)
}

// FileExists returns whether or not the file exists on the current file
// system.
func FileExists(name string) (bool, error) {
	_, err := os.Stat(name)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// UserHomeDir returns the user's home directory
func UserHomeDir() string {
	if runtime.GOOS == "windows" {
		home := os.Getenv("HOMEDRIVE") + os.Getenv("HOMEPATH")
		if home == "" {
			home = os.Getenv("USERPROFILE")
		}
		return home
	}
	return os.Getenv("HOME")
}

// AbsPath returns an absolute path relative.
func AbsPath(inPath string) string {
	if strings.HasPrefix(inPath, "~") {
		inPath = UserHomeDir() + inPath[len("~"):]
	} else if strings.HasPrefix(inPath, "$HOME") {
		inPath = UserHomeDir() + inPath[len("$HOME"):]
	}

	if strings.HasPrefix(inPath, "$") {
		end := strings.Index(inPath, string(os.PathSeparator))
		inPath = os.Getenv(inPath[1:end]) + inPath[end:]
	}

	if filepath.IsAbs(inPath) {
		return filepath.Clean(inPath)
	}

	p, err := filepath.Abs(inPath)
	if err == nil {
		return filepath.Clean(p)
	}

	return ""
}
