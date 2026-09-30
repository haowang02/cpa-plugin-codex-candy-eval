package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
)

type persistedState struct {
	Results      map[string][]candyResult       `json:"results"`
	Fingerprints map[string][]fingerprintResult `json:"fingerprints"`
	ModelTraces  map[string][]traceResult       `json:"modeltraces,omitempty"`
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
	if state.ModelTraces != nil {
		traceResults = state.ModelTraces
	}
	for id, history := range candyResults {
		candyResults[id] = history[max(0, len(history)-candyHistoryLimit):]
	}
	for id, history := range fingerprintResults {
		fingerprintResults[id] = history[max(0, len(history)-fingerprintHistoryLimit):]
	}
	for id, history := range traceResults {
		traceResults[id] = history[max(0, len(history)-traceHistoryLimit):]
	}
}

// Call with mu held so all histories are written as one snapshot.
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
	data, err := json.Marshal(persistedState{Results: candyResults, Fingerprints: fingerprintResults, ModelTraces: traceResults})
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

func recordResponse(scope string, query url.Values) managementResponse {
	authID, id := query.Get("auth_id"), query.Get("id")
	mu.Lock()
	defer mu.Unlock()
	var record any
	switch scope {
	case "fingerprint":
		if i := slices.IndexFunc(fingerprintResults[authID], func(r fingerprintResult) bool { return r.ID == id }); i >= 0 {
			record = fingerprintResults[authID][i]
		}
	case "modeltrace":
		if i := slices.IndexFunc(traceResults[authID], func(r traceResult) bool { return r.ID == id }); i >= 0 {
			record = traceResults[authID][i]
		}
	}
	if record == nil {
		return jsonError(http.StatusNotFound, "记录不存在，可能已被清空")
	}
	return jsonResponse(http.StatusOK, record)
}

func clearHistoryResponse(scope string) managementResponse {
	mu.Lock()
	defer mu.Unlock()
	previous := persistedState{Results: candyResults, Fingerprints: fingerprintResults, ModelTraces: traceResults}
	switch scope {
	case "fingerprint":
		fingerprintResults = map[string][]fingerprintResult{}
	case "modeltrace":
		traceResults = map[string][]traceResult{}
	case "candy":
		candyResults = map[string][]candyResult{}
	default:
		return jsonError(http.StatusBadRequest, "未知测试类型")
	}
	if err := saveStateLocked(); err != nil {
		candyResults, fingerprintResults = previous.Results, previous.Fingerprints
		traceResults = previous.ModelTraces
		return jsonError(http.StatusInternalServerError, "清空记录失败："+err.Error())
	}
	return jsonResponse(http.StatusOK, map[string]bool{"cleared": true})
}
