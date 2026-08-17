package contextflow

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func prepareNamedState(contextDir string) error {
	contextsDir := filepath.Dir(contextDir)
	serviceRoot := filepath.Dir(contextsDir)
	if err := prepareServiceRoot(serviceRoot); err != nil {
		return err
	}
	if err := createAndValidateDir(contextsDir, dirPerm, true); err != nil {
		return err
	}
	return createAndValidateDir(contextDir, dirPerm, true)
}

func prepareLegacyState(serviceRoot string) error {
	if serviceRoot == "" {
		return fmt.Errorf("context path is unavailable")
	}
	projectRoot := filepath.Dir(serviceRoot)
	base := filepath.Dir(projectRoot)
	if err := os.MkdirAll(base, dirPerm); err != nil {
		return fmt.Errorf("create state base directory: %w", err)
	}
	if err := createAndValidateDir(projectRoot, dirPerm, false); err != nil {
		return err
	}
	return createAndValidateDir(serviceRoot, dirPerm, false)
}

func prepareServiceRoot(serviceRoot string) error {
	if serviceRoot == "" {
		return fmt.Errorf("context path is unavailable")
	}
	projectRoot := filepath.Dir(serviceRoot)
	base := filepath.Dir(projectRoot)
	if err := os.MkdirAll(base, dirPerm); err != nil {
		return fmt.Errorf("create state base directory: %w", err)
	}
	if err := createAndValidateDir(projectRoot, dirPerm, false); err != nil {
		return err
	}
	return createAndValidateDir(serviceRoot, dirPerm, true)
}

func validateProjectRoot(serviceRoot string) error {
	projectRoot := filepath.Dir(serviceRoot)
	info, err := os.Lstat(projectRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat context directory: %w", err)
	}
	return validateDir(projectRoot, info, false)
}

func createAndValidateDir(path string, perm os.FileMode, private bool) error {
	err := os.Mkdir(path, perm)
	if err != nil && !os.IsExist(err) {
		return fmt.Errorf("create context directory: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat context directory: %w", err)
	}
	return validateDir(path, info, private)
}

func validateSecureDir(path string, info os.FileInfo) error {
	return validateDir(path, info, true)
}

func validateDir(path string, info os.FileInfo, private bool) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("context directory must be a directory, not a symlink: %s", path)
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	if private && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("context directory permissions must be 0700 or stricter: %s", path)
	}
	if !private && info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("context directory must not be group- or other-writable: %s", path)
	}
	return nil
}

func validateSecureFile(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("current context must be a regular file, not a symlink: %s", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("current context permissions must be 0600 or stricter: %s", path)
	}
	return nil
}
