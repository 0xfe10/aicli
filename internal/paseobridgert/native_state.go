package paseobridgert

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var nativeAgentID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
var nativeID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)

type nativeGuard struct {
	Version   int    `json:"version"`
	Host      string `json:"host"`
	Agent     string `json:"agent"`
	RequestID string `json:"requestId"`
	CreatedAt string `json:"createdAt"`
}

func nativeSecureDir(dir string) error {
	return nativeSecureDirWithSync(dir, nativeSyncDir)
}
func nativeSecureDirWithSync(dir string, syncParent func(string) error) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("native state directory must be absolute")
	}
	dir = filepath.Clean(dir)
	paths := []string{}
	// Validate the complete existing chain without following any symlink.
	for path := dir; path != filepath.Dir(path); path = filepath.Dir(path) {
		paths = append(paths, path)
		if info, err := os.Lstat(path); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return fmt.Errorf("native state directory ancestors must be real directories")
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	// Finish and persist ancestors before creating descendants. Sync even when
	// another writer created an entry but has not yet persisted its parent.
	for i := len(paths) - 1; i >= 0; i-- {
		path := paths[i]
		created := false
		if err := os.Mkdir(path, 0700); err == nil {
			created = true
		} else if !os.IsExist(err) {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("native state directory ancestors must be real directories")
		}
		if created || path == dir {
			if err = rejectInsecureDir(path, info); err != nil {
				return err
			}
		}
		if err = syncParent(filepath.Dir(path)); err != nil {
			return err
		}
	}
	if dir == filepath.Dir(dir) {
		return fmt.Errorf("native state directory cannot be filesystem root")
	}
	return nil
}
func nativeSyncDir(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
func nativeCreateFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return nativeSyncDir(filepath.Dir(path))
}
func nativeClientID(stateDir, host string) (string, error) {
	if err := nativeSecureDir(stateDir); err != nil {
		return "", err
	}
	path := filepath.Join(stateDir, host+".client-id")
	raw, err := protectedRead(path)
	if os.IsNotExist(err) {
		id := nativeUUID()
		if id == "" {
			return "", nativeFailure
		}
		err = nativeCreateFile(path, []byte(id))
		if err == nil {
			return id, nil
		}
		if os.IsExist(err) {
			raw, err = protectedRead(path)
		}
	}
	if err != nil || !nativeID.Match(raw) {
		return "", fmt.Errorf("native client identity file is unavailable or invalid")
	}
	return string(raw), nil
}
func nativeGuardPath(stateDir, host, agent string) (string, error) {
	if !hostName.MatchString(host) || !nativeAgentID.MatchString(agent) {
		return "", fmt.Errorf("invalid native send target")
	}
	dir := filepath.Join(stateDir, "submissions")
	if err := nativeSecureDir(stateDir); err != nil {
		return "", err
	}
	if err := nativeSecureDir(dir); err != nil {
		return "", err
	}
	return filepath.Join(dir, host+"."+agent+".json"), nil
}
func nativePrepareGuard(stateDir, host, agent, id string) (string, error) {
	path, err := nativeGuardPath(stateDir, host, agent)
	if err != nil {
		return "", err
	}
	record := nativeGuard{1, host, agent, id, nativeFetchedAt()}
	raw, _ := json.Marshal(record)
	if err = nativeCreateFile(path, raw); err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("earlier send remains unresolved; local operator reconciliation is required")
		}
		return "", fmt.Errorf("could not durably record send guard; nothing was sent")
	}
	return path, nil
}
func nativeClearGuard(path, host, agent, id string) error {
	raw, err := protectedRead(path)
	if err != nil {
		return err
	}
	var guard nativeGuard
	if strictJSON(raw, &guard) != nil || guard.Version != 1 || guard.Host != host || guard.Agent != agent || guard.RequestID != id {
		return fmt.Errorf("native send guard identity mismatch")
	}
	if err = os.Remove(path); err != nil {
		return err
	}
	if err = nativeSyncDir(filepath.Dir(path)); err != nil {
		_ = nativeCreateFile(path, raw)
		return err
	}
	return nil
}
