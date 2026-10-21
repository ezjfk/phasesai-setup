package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxConfigBytes = 2 << 20

type fileMetadata struct {
	Mode     uint32 `json:"mode"`
	Security string `json:"security,omitempty"`
}
type transactionJournal struct {
	Version  int          `json:"version"`
	Before   string       `json:"before_sha256"`
	After    string       `json:"after_sha256"`
	Existed  bool         `json:"existed"`
	Backup   string       `json:"backup"`
	Stage    string       `json:"stage"`
	Metadata fileMetadata `json:"metadata"`
}
type configStore struct {
	home, parent *os.Root
	lock         *os.File
	name         string
}

func digest(data []byte) string { hash := sha256.Sum256(data); return hex.EncodeToString(hash[:]) }
func nonce() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func localTarget(home, target string) (string, error) {
	home, err := filepath.Abs(home)
	if err != nil {
		return "", err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(home, target)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return "", errors.New("config must be inside your home directory")
	}
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if strings.Contains(part, ":") || strings.TrimSpace(part) != part || strings.HasSuffix(part, ".") {
			return "", errors.New("ambiguous config path requires manual setup")
		}
	}
	return rel, nil
}

func safePath(home, target string) error {
	rel, err := localTarget(home, target)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return err
	}
	defer root.Close()
	current := ""
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || isReparse(info) {
			return errors.New("linked or reparse-point config paths require manual setup")
		}
	}
	return nil
}

func openStore(home, target string) (*configStore, error) {
	if err := safePath(home, target); err != nil {
		return nil, err
	}
	rel, err := localTarget(home, target)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return nil, err
	}
	directory := filepath.Dir(rel)
	if err = root.MkdirAll(directory, 0700); err != nil {
		root.Close()
		return nil, err
	}
	parent, err := root.OpenRoot(directory)
	if err != nil {
		root.Close()
		return nil, err
	}
	store := &configStore{home: root, parent: parent, name: filepath.Base(rel)}
	lock, err := parent.OpenFile(store.name+".phasesai.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		store.close()
		return nil, err
	}
	store.lock = lock
	if _, err = inspectFile(lock); err != nil {
		store.close()
		return nil, err
	}
	if err = secureFile(lock); err != nil {
		store.close()
		return nil, err
	}
	if err = lockFile(lock); err != nil {
		store.close()
		return nil, errors.New("another setup process holds this config lock")
	}
	return store, nil
}
func (s *configStore) close() {
	if s.lock != nil {
		s.lock.Close()
	}
	if s.parent != nil {
		s.parent.Close()
	}
	if s.home != nil {
		s.home.Close()
	}
}

func (s *configStore) read(name string) ([]byte, bool, fileMetadata, error) {
	var metadata fileMetadata
	info, err := s.parent.Lstat(name)
	if os.IsNotExist(err) {
		return nil, false, metadata, nil
	}
	if err != nil {
		return nil, false, metadata, err
	}
	if !info.Mode().IsRegular() || isReparse(info) {
		return nil, false, metadata, errors.New("config or recovery file must be a regular unlinked file")
	}
	f, err := s.parent.Open(name)
	if err != nil {
		return nil, false, metadata, err
	}
	defer f.Close()
	metadata, err = inspectFile(f)
	if err != nil {
		return nil, false, metadata, err
	}
	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil {
		return nil, false, metadata, err
	}
	if len(data) > maxConfigBytes {
		return nil, false, metadata, errors.New("configuration or journal exceeds 2 MiB")
	}
	return data, true, metadata, nil
}

func (s *configStore) exclusive(name string, data []byte, metadata *fileMetadata) error {
	f, err := s.parent.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	failed := true
	defer func() {
		f.Close()
		if failed {
			s.parent.Remove(name)
		}
	}()
	if err = secureFile(f); err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if metadata != nil {
		if err = restoreMetadata(f, *metadata); err != nil {
			return err
		}
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	failed = false
	return nil
}
func (s *configStore) sync() error         { return syncDirectory(s.parent) }
func (s *configStore) journalName() string { return s.name + ".phasesai-journal.json" }

func (s *configStore) loadJournal() (*transactionJournal, error) {
	data, exists, _, err := s.read(s.journalName())
	if err != nil || !exists {
		return nil, err
	}
	var j transactionJournal
	if json.Unmarshal(data, &j) != nil || j.Version != 1 || len(j.Before) != 64 || len(j.After) != 64 {
		return nil, errors.New("invalid recovery journal; preserve files and restore the backup manually")
	}
	for _, name := range []string{j.Backup, j.Stage} {
		if name == "" {
			continue
		}
		if filepath.Base(name) != name || !strings.HasPrefix(name, s.name+".phasesai-") {
			return nil, errors.New("unsafe recovery filename")
		}
	}
	if j.Existed && (j.Backup == "" || !strings.HasSuffix(j.Backup, ".bak")) {
		return nil, errors.New("recovery backup missing")
	}
	return &j, nil
}

func transact(path string, before, after []byte, existed bool, verify func() error) (string, error) {
	return transactHome(filepath.Dir(path), path, before, after, existed, verify)
}

func transactHome(home, path string, before, after []byte, existed bool, verify func() error) (string, error) {
	s, err := openStore(home, path)
	if err != nil {
		return "", err
	}
	defer s.close()
	j, err := s.loadJournal()
	if err != nil {
		return "", err
	}
	if j != nil {
		return "", errors.New("interrupted transaction found; use --recover before making another change")
	}
	current, exists, metadata, err := s.read(s.name)
	if err != nil {
		return "", err
	}
	if exists != existed || !bytes.Equal(current, before) {
		return "", errors.New("config changed since preview; retry")
	}
	if bytes.Equal(before, after) {
		return "", verify()
	}
	id := nonce()
	backup := ""
	if existed {
		backup = s.name + ".phasesai-" + id + ".bak"
		if err = s.exclusive(backup, before, nil); err != nil {
			return "", err
		}
	}
	stage := s.name + ".phasesai-" + id + ".stage"
	stageMetadata := metadata
	if !existed {
		stageMetadata.Mode = 0600
	}
	if err = s.exclusive(stage, after, &stageMetadata); err != nil {
		return backupPath(path, backup), err
	}
	journal := transactionJournal{1, digest(before), digest(after), existed, backup, stage, metadata}
	raw, _ := json.Marshal(journal)
	if err = s.exclusive(s.journalName(), raw, nil); err != nil {
		s.parent.Remove(stage)
		return backupPath(path, backup), err
	}
	if err = s.sync(); err != nil {
		return backupPath(path, backup), err
	}
	current, exists, _, err = s.read(s.name)
	if err != nil || exists != existed || !bytes.Equal(current, before) {
		s.parent.Remove(stage)
		s.parent.Remove(s.journalName())
		return backupPath(path, backup), errors.New("config changed before replacement; no config applied")
	}
	if err = s.parent.Rename(stage, s.name); err != nil {
		return backupPath(path, backup), err
	}
	if err = s.sync(); err != nil {
		return backupPath(path, backup), err
	}
	if err = verify(); err != nil {
		if recoverErr := s.recover(&journal); recoverErr != nil {
			return backupPath(path, backup), fmt.Errorf("verification failed; %w", recoverErr)
		}
		return backupPath(path, backup), errors.New("verification failed; previous config restored")
	}
	if err = s.parent.Remove(s.journalName()); err != nil {
		return backupPath(path, backup), err
	}
	return backupPath(path, backup), s.sync()
}

func backupPath(path, name string) string {
	if name == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(path), name)
}

func (s *configStore) recover(j *transactionJournal) error {
	current, exists, _, err := s.read(s.name)
	if err != nil {
		return err
	}
	beforeMatches := exists == j.Existed && digest(current) == j.Before
	if !beforeMatches {
		if !exists || digest(current) != j.After {
			return errors.New("config changed after installation; preserve it and restore backup manually")
		}
		if j.Existed {
			original, ok, _, err := s.read(j.Backup)
			if err != nil || !ok || digest(original) != j.Before {
				return errors.New("backup integrity check failed; restore manually")
			}
			temp := s.name + ".phasesai-" + nonce() + ".restore"
			if err = s.exclusive(temp, original, &j.Metadata); err != nil {
				return err
			}
			if err = s.parent.Rename(temp, s.name); err != nil {
				return err
			}
		} else {
			if err = s.parent.Remove(s.name); err != nil {
				return err
			}
		}
	}
	if j.Stage != "" {
		if err = s.parent.Remove(j.Stage); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err = s.parent.Remove(s.journalName()); err != nil {
		return err
	}
	return s.sync()
}
func recoverTransaction(home, path string) error {
	s, err := openStore(home, path)
	if err != nil {
		return err
	}
	defer s.close()
	j, err := s.loadJournal()
	if err != nil {
		return err
	}
	if j == nil {
		return errors.New("no interrupted transaction exists for this config")
	}
	return s.recover(j)
}
