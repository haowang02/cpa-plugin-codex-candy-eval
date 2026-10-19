package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// stateSchemaVersion is the layout of the state file the plugin writes. Files without schema_version are
// version 0.
const stateSchemaVersion = 2

// stateMigrations[v] upgrades the raw JSON of a state file from schema v to v+1.
var stateMigrations = []func(file map[string]json.RawMessage) error{
	migrateUnversionedState,
	addPelicanVariants,
}

// readState decodes a state file of the current or an older schema, and returns the file's own version.
func readState(data []byte) (s state, version int, err error) {
	var file map[string]json.RawMessage
	if err := json.Unmarshal(data, &file); err != nil {
		return s, 0, err
	}
	if file == nil {
		return s, 0, errors.New("测试记录格式无效")
	}
	if raw, ok := file["schema_version"]; ok {
		if err := json.Unmarshal(raw, &version); err != nil || version < 0 {
			return s, 0, fmt.Errorf("测试记录版本无效：%s", raw)
		}
	}
	if version > stateSchemaVersion {
		return s, version, fmt.Errorf("测试记录来自更新版本的插件（schema %d）", version)
	}
	for v := version; v < stateSchemaVersion; v++ {
		if err := stateMigrations[v](file); err != nil {
			return s, version, fmt.Errorf("测试记录从 schema %d 迁移失败：%w", v, err)
		}
	}
	if data, err = json.Marshal(file); err == nil {
		err = json.Unmarshal(data, &s)
	}
	s.SchemaVersion = stateSchemaVersion
	s.Candies, s.Fingerprints, s.Pelicans = recent(s.Candies), recent(s.Fingerprints), recent(s.Pelicans)
	return s, version, err
}

// migrateUnversionedState upgrades the files of plugin 0.4.0 and earlier. Candy results move from
// "results" to "candies" and fingerprint detections from "detections" to "fingerprints"; candy records get
// an ID, and candy and pelican records a status in place of their ok and skipped flags. The probe-test
// records that releases before 0.4.0 kept under "fingerprints", and their ModelTrace records, are dropped.
func migrateUnversionedState(file map[string]json.RawMessage) error {
	candies, err := mapRecords(file["results"], func(r map[string]any) error {
		at, err := time.Parse(time.RFC3339Nano, fmt.Sprint(r["time"]))
		answered := "wrong"
		if r["ok"] == true {
			answered = "correct"
		}
		r["id"] = fmt.Sprint(at.UnixNano())
		setStatus(r, answered)
		delete(r, "ok")
		return err
	})
	if err != nil {
		return err
	}
	pelicans, err := mapRecords(file["pelicans"], func(r map[string]any) error {
		setStatus(r, "completed")
		return nil
	})
	if err != nil {
		return err
	}
	fingerprints := file["detections"]
	clear(file)
	file["candies"], file["fingerprints"], file["pelicans"] = candies, fingerprints, pelicans
	return nil
}

// setStatus gives an unversioned candy or pelican record its status in place of the skipped flag; a
// record neither skipped nor failed got answered.
func setStatus(r map[string]any, answered string) {
	switch {
	case r["skipped"] == true:
		r["status"] = "skipped"
	case r["error"] != nil && r["error"] != "":
		r["status"] = "failed"
	default:
		r["status"] = answered
	}
	delete(r, "skipped")
}

// addPelicanVariants marks the pelican records of plugin 0.4.1 and earlier, drawn before the test had
// variants, as drawn by the original variant.
func addPelicanVariants(file map[string]json.RawMessage) (err error) {
	file["pelicans"], err = mapRecords(file["pelicans"], func(r map[string]any) error {
		r["variant"] = "original"
		return nil
	})
	return err
}

// mapRecords applies change to every record of a history in raw JSON, keeping numbers as written.
func mapRecords(raw json.RawMessage, change func(map[string]any) error) (json.RawMessage, error) {
	if raw == nil {
		return nil, nil
	}
	var h map[string][]map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&h); err != nil {
		return nil, err
	}
	for _, records := range h {
		for _, r := range records {
			if r == nil {
				return nil, errors.New("记录格式无效")
			}
			if err := change(r); err != nil {
				return nil, err
			}
		}
	}
	return json.Marshal(h)
}
