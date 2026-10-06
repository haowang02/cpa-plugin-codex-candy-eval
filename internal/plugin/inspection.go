package plugin

import (
	"encoding/json"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"
)

type inspection struct {
	started time.Time
	authIDs []string
}

type inspectionItem struct {
	credential
	Results any `json:"results"`
	// Degraded is null when no record from this inspection reaches a verdict, e.g. its requests failed.
	Degraded *bool `json:"degraded"`
}

var inspectionRuns = map[string]func([]byte) managementResponse{
	"candy": candyRunResponse, "fingerprint": fingerprintRunResponse, "modeltrace": traceRunResponse,
}

// The latest inspection of each test, kept until the plugin stops.
var inspections = map[string]inspection{}

// inspectionRunResponse starts the test named in body on the enabled credentials whose email or name
// matches an include pattern (any credential when there are none) and no exclude pattern. The rest of
// body is that test's run request.
func inspectionRunResponse(body []byte) managementResponse {
	var req struct {
		Test    string   `json:"test"`
		Include []string `json:"include"`
		Exclude []string `json:"exclude"`
	}
	if json.Unmarshal(body, &req) != nil {
		return jsonError(http.StatusBadRequest, "请求格式错误")
	}
	run := inspectionRuns[req.Test]
	if run == nil {
		return jsonError(http.StatusBadRequest, "未知测试类型")
	}
	auths, err := selectedCredentials(nil, true)
	if err != nil {
		return jsonError(http.StatusBadGateway, err.Error())
	}
	ids := []string{}
	for _, auth := range auths {
		if (len(req.Include) == 0 || matchesAny(auth, req.Include)) && !matchesAny(auth, req.Exclude) {
			ids = append(ids, auth.ID)
		}
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(body, &fields)
	fields["auth_ids"], _ = json.Marshal(ids)
	delete(fields, "all")
	body, _ = json.Marshal(fields)
	started := time.Now()
	response := run(body)
	if response.StatusCode == http.StatusOK {
		mu.Lock()
		inspections[req.Test] = inspection{started, ids}
		mu.Unlock()
	}
	return response
}

func matchesAny(auth credential, patterns []string) bool {
	for _, pattern := range patterns {
		for _, value := range []string{auth.Email, auth.Name} {
			if matched, _ := path.Match(pattern, value); matched {
				return true
			}
		}
	}
	return false
}

// inspectionResponse reports whether the latest inspection of test is still running, and the records each
// of its credentials got from it. A credential is degraded when any of them shows so: a wrong candy
// answer, or a fingerprint or ModelTrace attribution to another model.
func inspectionResponse(test string) managementResponse {
	auths, err := credentials()
	if err != nil {
		return jsonError(http.StatusBadGateway, err.Error())
	}
	mu.Lock()
	defer mu.Unlock()
	latest, ok := inspections[test]
	if !ok {
		return jsonError(http.StatusNotFound, "还没有开始过这项巡检")
	}
	running, items := false, []inspectionItem{}
	for _, auth := range auths {
		if !slices.Contains(latest.authIDs, auth.ID) {
			continue
		}
		item := inspectionItem{credential: auth}
		switch test {
		case "candy":
			running = running || candyRunning[auth.ID] != nil
			item.Results, item.Degraded = inspected(candyResults[auth.ID], latest.started, func(r candyResult) (time.Time, bool, bool) {
				return r.Time, !r.Skipped && r.Error == "", !r.OK
			})
		case "fingerprint":
			running = running || fingerprintRunning[auth.ID] != nil
			records, degraded := inspected(fingerprintResults[auth.ID], latest.started, func(r fingerprintResult) (time.Time, bool, bool) {
				status := r.Attribution.Status
				return r.Time, r.Status == "completed" && (status == "consistent" || status == "substitution" || status == "different"), status != "consistent"
			})
			item.Results, item.Degraded = summaries(records), degraded
		case "modeltrace":
			running = running || traceRunning[auth.ID] != nil
			records, degraded := inspected(traceResults[auth.ID], latest.started, func(r traceResult) (time.Time, bool, bool) {
				if r.Attribution == nil || (r.Status != "completed" && r.Status != "partial") {
					return r.Time, false, false
				}
				return r.Time, true, traceModelID(r.Model) != traceModelID(r.Attribution.Prediction)
			})
			item.Results, item.Degraded = summaries(records), degraded
		}
		items = append(items, item)
	}
	return jsonResponse(http.StatusOK, map[string]any{"running": running, "credentials": items})
}

// inspected returns the records of history made since started, and whether any of them shows a degraded
// model, or nil when none reaches a verdict. judge returns when a record was made and its verdict.
func inspected[T any](history []T, started time.Time, judge func(T) (at time.Time, concluded, degraded bool)) ([]T, *bool) {
	records, anyConcluded, anyDegraded := []T{}, false, false
	for _, r := range history {
		at, concluded, degraded := judge(r)
		if at.Before(started) {
			continue
		}
		records = append(records, r)
		anyConcluded = anyConcluded || concluded
		anyDegraded = anyDegraded || concluded && degraded
	}
	if !anyConcluded {
		return records, nil
	}
	return records, &anyDegraded
}

// traceModelID drops a provider prefix such as "openai/", as the page does when comparing models.
func traceModelID(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	return model[strings.LastIndex(model, "/")+1:]
}
