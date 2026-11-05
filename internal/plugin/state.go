package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

// Each credential keeps its latest historyLimit records of every test.
const historyLimit = 20

// history holds the records of one test per credential ID, oldest first.
type history[T any] map[string][]T

func (h history[T]) add(id string, r T) {
	records := append(h[id], r)
	h[id] = records[max(0, len(records)-historyLimit):]
}

// recent returns a non-nil copy of h with at most historyLimit records per credential.
func recent[T any](h history[T]) history[T] {
	out := history[T]{}
	for id, records := range h {
		out[id] = records[max(0, len(records)-historyLimit):]
	}
	return out
}

// state is everything the plugin persists, in the layout of schema stateSchemaVersion.
type state struct {
	SchemaVersion int                        `json:"schema_version"`
	Candies       history[candyRecord]       `json:"candies"`
	Fingerprints  history[fingerprintRecord] `json:"fingerprints"`
	Pelicans      history[pelicanRecord]     `json:"pelicans"`
	SIWC          history[siwcRecord]        `json:"siwc"`
}

func emptyState() state {
	return state{SchemaVersion: stateSchemaVersion, Candies: history[candyRecord]{}, Fingerprints: history[fingerprintRecord]{}, Pelicans: history[pelicanRecord]{}, SIWC: history[siwcRecord]{}}
}

// record is what the records of every test have.
type record struct {
	ID         string    `json:"id"`
	Time       time.Time `json:"time"`
	Model      string    `json:"model"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	DurationMS int64     `json:"duration_ms"`
	usage
}

func (r record) base() record { return r }

func newRecord(model string, at time.Time) record {
	return record{ID: fmt.Sprint(at.UnixNano()), Time: at.UTC(), Model: model}
}

const unsupportedModelMessage = "已跳过：此凭证不支持所选模型"

func skippedRecord(model string) record {
	r := newRecord(model, time.Now())
	r.Status, r.Error = "skipped", unsupportedModelMessage
	return r
}

var (
	statePath = "plugins/" + pluginID + "-state.json"
	// records is the state of every test, guarded by mu.
	records = emptyState()
	loaded  bool
	// stateLoadError keeps a state file the plugin could not read or back up from being replaced.
	stateLoadError error
	storageError   string
)

// loadState reads the state file once. A file of an older schema is migrated, backed up beside the state
// file as it was, and saved in the current schema.
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
	var s state
	var version int
	if err == nil {
		s, version, err = readState(data)
	}
	if err != nil {
		blockSaving("测试记录读取失败", err)
		return
	}
	records = s
	if version < stateSchemaVersion {
		backup := fmt.Sprintf("%s.v%d.json", strings.TrimSuffix(statePath, ".json"), version)
		if err := os.WriteFile(backup, data, 0o600); err != nil {
			blockSaving("旧版测试记录备份失败", err)
			return
		}
		_ = saveStateLocked()
	}
}

func blockSaving(reason string, err error) {
	stateLoadError = fmt.Errorf("%s，为保护原文件已暂停保存：%w", reason, err)
	storageError = stateLoadError.Error()
}

// saveStateLocked writes all records as one snapshot; call it with mu held.
func saveStateLocked() error {
	if stateLoadError != nil {
		return stateLoadError
	}
	err := writeState()
	storageError = ""
	if err != nil {
		storageError = "测试记录保存失败：" + err.Error()
	}
	return err
}

func writeState() error {
	data, err := json.Marshal(records)
	if err != nil {
		return err
	}
	tmp := statePath + ".tmp"
	defer os.Remove(tmp)
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, statePath)
}

func recordResponse(test string, query url.Values) managementResponse {
	credentialID, id := query.Get("credential_id"), query.Get("id")
	mu.Lock()
	defer mu.Unlock()
	var found any
	switch test {
	case "fingerprint":
		found = findRecord(records.Fingerprints[credentialID], id)
	case "pelican":
		found = findRecord(records.Pelicans[credentialID], id)
	}
	if found == nil {
		return jsonError(http.StatusNotFound, "记录不存在，可能已被清空")
	}
	return jsonResponse(http.StatusOK, found)
}

// findRecord returns the record with id, or nil.
func findRecord[T interface{ base() record }](history []T, id string) any {
	if i := slices.IndexFunc(history, func(r T) bool { return r.base().ID == id }); i >= 0 {
		return history[i]
	}
	return nil
}

// clearRecordsResponse deletes the records of test, or of every test for "all".
func clearRecordsResponse(test string) managementResponse {
	mu.Lock()
	defer mu.Unlock()
	previous := records
	switch test {
	case "siwc":
		records.SIWC = history[siwcRecord]{}
	case "candy":
		records.Candies = history[candyRecord]{}
	case "fingerprint":
		records.Fingerprints = history[fingerprintRecord]{}
	case "pelican":
		records.Pelicans = history[pelicanRecord]{}
	case "all":
		records = emptyState()
	default:
		return jsonError(http.StatusBadRequest, "未知测试类型")
	}
	if err := saveStateLocked(); err != nil {
		records = previous
		return jsonError(http.StatusInternalServerError, "清空测试记录失败："+err.Error())
	}
	return jsonResponse(http.StatusOK, map[string]bool{"cleared": true})
}
