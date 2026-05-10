package testreport

import (
	"encoding/json"
	"os"
)

type Status string

const (
	StatusNotRun Status = "not_run"
	StatusPassed Status = "passed"
	StatusFailed Status = "failed"
)

type Report struct {
	Status  Status   `json:"status"`
	RunAt   string   `json:"run_at"`
	Total   int      `json:"total"`
	Passed  int      `json:"passed"`
	Failed  int      `json:"failed"`
	Results []Result `json:"results"`
}

type Result struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Duration string `json:"duration"`
	Message  string `json:"message,omitempty"`
}

func Write(path string, r Report) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(r); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
