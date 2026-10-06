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
	Pelicans     map[string][]pelicanResult     `json:"pelicans,omitempty"`
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
	var state *persistedState
	if err == nil {
		err = json.Unmarshal(data, &state)
		if err == nil && state == nil {
			err = errors.New("测试记录格式无效")
		}
	}
	if err != nil {
		stateLoadError = fmt.Errorf("测试记录读取失败，为保护原文件已暂停保存：%w", err)
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
	if state.Pelicans != nil {
		pelicanResults = state.Pelicans
	}
	keepRecent(candyResults)
	keepRecent(fingerprintResults)
	keepRecent(traceResults)
	keepRecent(pelicanResults)
}

// Each credential keeps its latest historyLimit records of every test.
const historyLimit = 20

func appendRecent[T any](history []T, r T) []T {
	history = append(history, r)
	return history[max(0, len(history)-historyLimit):]
}

func keepRecent[T any](histories map[string][]T) {
	for id, history := range histories {
		histories[id] = history[max(0, len(history)-historyLimit):]
	}
}

func currentState() persistedState {
	return persistedState{Results: candyResults, Fingerprints: fingerprintResults, ModelTraces: traceResults, Pelicans: pelicanResults}
}

// Call with mu held so all histories are written as one snapshot.
func saveStateLocked() (err error) {
	defer func() {
		storageError = ""
		if err != nil {
			storageError = "测试记录保存失败：" + err.Error()
		}
	}()
	if stateLoadError != nil {
		return stateLoadError
	}
	data, err := json.Marshal(currentState())
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
	case "pelican":
		if i := slices.IndexFunc(pelicanResults[authID], func(r pelicanResult) bool { return r.ID == id }); i >= 0 {
			record = pelicanResults[authID][i]
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
	previous := currentState()
	switch scope {
	case "fingerprint":
		fingerprintResults = map[string][]fingerprintResult{}
	case "modeltrace":
		traceResults = map[string][]traceResult{}
	case "candy":
		candyResults = map[string][]candyResult{}
	case "pelican":
		pelicanResults = map[string][]pelicanResult{}
	case "all":
		candyResults, fingerprintResults = map[string][]candyResult{}, map[string][]fingerprintResult{}
		traceResults, pelicanResults = map[string][]traceResult{}, map[string][]pelicanResult{}
	default:
		return jsonError(http.StatusBadRequest, "未知测试类型")
	}
	if err := saveStateLocked(); err != nil {
		candyResults, fingerprintResults = previous.Results, previous.Fingerprints
		traceResults, pelicanResults = previous.ModelTraces, previous.Pelicans
		return jsonError(http.StatusInternalServerError, "清空测试记录失败："+err.Error())
	}
	return jsonResponse(http.StatusOK, map[string]bool{"cleared": true})
}
