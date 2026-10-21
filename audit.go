package main

import (
	"encoding/json"
	"errors"
	"os"
	"time"
)

type auditLog struct{ store *configStore }

func openAudit(home, path string) (*auditLog, error) {
	s, err := openStore(home, path)
	if err != nil {
		return nil, err
	}
	return &auditLog{s}, nil
}
func (a *auditLog) close() {
	if a != nil {
		a.store.close()
	}
}
func (a *auditLog) event(step, result, harness string) error {
	if a == nil {
		return nil
	}
	// Only program-controlled event enums enter logs. Never serialize errors,
	// config paths, commands, environment, keys, HTTP bodies or prompts.
	allowed := map[string]bool{"start": true, "apply": true, "recover": true, "complete": true, "failed": true, "ok": true}
	if !allowed[step] || !allowed[result] {
		return errors.New("invalid audit event")
	}
	s := a.store
	data, exists, _, err := s.read(s.name)
	if err != nil {
		return err
	}
	if exists && len(data) >= 1<<20 {
		if err = s.parent.Rename(s.name, s.name+".1"); err != nil {
			return err
		}
	}
	f, err := s.parent.OpenFile(s.name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = secureFile(f); err != nil {
		return err
	}
	line, _ := json.Marshal(map[string]string{"time": time.Now().UTC().Format(time.RFC3339), "step": step, "result": result, "harness": harness, "version": version})
	if _, err = f.Write(append(line, '\n')); err != nil {
		return err
	}
	return f.Sync()
}
