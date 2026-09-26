package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
)

type persistedState struct {
	Results      map[string][]candyResult       `json:"results"`
	Fingerprints map[string][]fingerprintResult `json:"fingerprints"`
}

var (
	statePath      = "plugins/" + pluginID + "-state.json"
	loaded         bool
	stateLoadError error
	storageError   string
)

func loadState() {
	mu.Lock()
	defer mu.Unlock()
	if loaded {
		return
	}
	loaded = true
	data, err := os.ReadFile(statePath)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	var state persistedState
	if err == nil {
		err = json.Unmarshal(data, &state)
	}
	if err != nil {
		stateLoadError = fmt.Errorf("历史记录读取失败，为保护原文件已暂停保存：%w", err)
		storageError = stateLoadError.Error()
		return
	}
	if state.Results != nil {
		candyResults = state.Results
	}
	if state.Fingerprints != nil {
		fingerprintResults = state.Fingerprints
	}
	for id, history := range candyResults {
		candyResults[id] = history[max(0, len(history)-candyHistoryLimit):]
	}
	for id, history := range fingerprintResults {
		fingerprintResults[id] = history[max(0, len(history)-fingerprintHistoryLimit):]
	}
}

// Call with mu held so both histories are written as one snapshot.
func saveStateLocked() (err error) {
	defer func() {
		storageError = ""
		if err != nil {
			storageError = "历史记录未保存：" + err.Error()
		}
	}()
	if stateLoadError != nil {
		return stateLoadError
	}
	data, err := json.Marshal(persistedState{candyResults, fingerprintResults})
	if err != nil {
		return err
	}
	tmp := statePath + ".tmp"
	defer os.Remove(tmp)
	if err = os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, statePath)
}

func clearHistoryResponse(fingerprint bool) managementResponse {
	mu.Lock()
	defer mu.Unlock()
	previous := persistedState{candyResults, fingerprintResults}
	if fingerprint {
		fingerprintResults = map[string][]fingerprintResult{}
	} else {
		candyResults = map[string][]candyResult{}
	}
	if err := saveStateLocked(); err != nil {
		candyResults, fingerprintResults = previous.Results, previous.Fingerprints
		return jsonError(http.StatusInternalServerError, "清空记录失败："+err.Error())
	}
	return jsonResponse(http.StatusOK, map[string]bool{"cleared": true})
}
