package plugin

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatePersistence(t *testing.T) {
	setupTest(t)
	for i := range historyLimit + 5 {
		records.Candies.add("a", candyRecord{record: record{ID: fmt.Sprint(i), Status: "correct"}})
	}
	if err := saveStateLocked(); err != nil {
		t.Fatal(err)
	}
	records, loaded = emptyState(), false
	loadState()
	if history := records.Candies["a"]; len(history) != historyLimit || history[0].ID != "5" || records.SchemaVersion != stateSchemaVersion {
		t.Fatalf("restored history must keep the latest %d records: %+v", historyLimit, history)
	}
	running["a"] = &progress{Done: 1, Total: 2}
	clearCandies := managementRequest{Method: http.MethodDelete, Path: managementBase + "/candy/records"}
	if response := handleManagement(clearCandies); response.StatusCode != http.StatusOK || len(records.Candies) != 0 || running["a"].Done != 1 {
		t.Fatalf("clear must remove records and keep runs going: %+v", response)
	}
	records, loaded = emptyState(), false
	loadState()
	if len(records.Candies) != 0 {
		t.Fatal("cleared records reappeared after reloading state")
	}
	records.Candies.add("keep", candyRecord{record: record{Status: "correct"}})
	statePath = filepath.Join(t.TempDir(), "missing", "state.json")
	if response := handleManagement(clearCandies); response.StatusCode != http.StatusInternalServerError || len(records.Candies["keep"]) != 1 || storageError == "" {
		t.Fatalf("a failed save must be reported and keep the records: %+v", response)
	}
}

func TestStateMigration(t *testing.T) {
	const at = "2026-10-19T04:49:34.427144308Z"
	for name, file := range map[string]string{
		"0.4.0": `{"results":{"a":[
				{"time":"` + at + `","model":"m","effort":"low","ok":true,"answer":"21","input_tokens":14125,"duration_ms":5},
				{"time":"` + at + `","model":"m","effort":"low","ok":false,"answer":"20"},
				{"time":"` + at + `","model":"m","error":"busy"},
				{"time":"` + at + `","model":"m","skipped":true,"error":"unsupported"}]},
			"detections":{"a":[{"id":"1","time":"` + at + `","model":"m","status":"partial"}]},
			"pelicans":{"a":[{"id":"2","time":"` + at + `","html":"<svg></svg>"},{"id":"3","time":"` + at + `","skipped":true,"error":"unsupported"}]}}`,
		"0.3": `{"results":{},"fingerprints":{"a":[{"id":"probe","mode":"probe","attribution":{"results":{}}}]},"modeltraces":{"a":[{}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			setupTest(t)
			if err := os.WriteFile(statePath, []byte(file), 0o600); err != nil {
				t.Fatal(err)
			}
			loadState()
			if name == "0.3" {
				if storageError != "" || len(records.Candies) != 0 || len(records.Fingerprints) != 0 {
					t.Fatalf("legacy probe records must be dropped: %+v %s", records, storageError)
				}
			} else {
				var statuses []string
				for _, r := range records.Candies["a"] {
					statuses = append(statuses, r.Status)
				}
				candy, pelicans := records.Candies["a"][0], records.Pelicans["a"]
				if strings.Join(statuses, " ") != "correct wrong failed skipped" || candy.ID != "1792385374427144308" || candy.InputTokens != 14125 ||
					records.Fingerprints["a"][0].Status != "partial" || pelicans[0].Status != "completed" || pelicans[1].Status != "skipped" || pelicans[1].Variant != "original" {
					t.Fatalf("migrated records: %+v", records)
				}
			}
			saved, _ := os.ReadFile(statePath)
			backup, _ := os.ReadFile(strings.TrimSuffix(statePath, ".json") + ".v0.json")
			if !bytes.Contains(saved, []byte(fmt.Sprintf(`"schema_version":%d`, stateSchemaVersion))) || bytes.Contains(saved, []byte(`"results"`)) || string(backup) != file {
				t.Fatalf("saved %s, backup %s", saved, backup)
			}
		})
	}
}

func TestUnreadableStateIsPreserved(t *testing.T) {
	for _, input := range []string{`{"candies":`, `null`, `{"schema_version":"1"}`, fmt.Sprintf(`{"schema_version":%d}`, stateSchemaVersion+1)} {
		t.Run(input, func(t *testing.T) {
			setupTest(t)
			data := []byte(input)
			if err := os.WriteFile(statePath, data, 0o600); err != nil {
				t.Fatal(err)
			}
			loadState()
			if storageError == "" || saveStateLocked() == nil {
				t.Fatal("an unreadable state file must report an error and block saving")
			}
			if got, err := os.ReadFile(statePath); err != nil || !bytes.Equal(got, data) {
				t.Fatal("an unreadable state file was overwritten")
			}
		})
	}
}
