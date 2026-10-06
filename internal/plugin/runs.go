package plugin

import (
	"context"
	"encoding/json"
	"net/http"
)

// Call with mu held; a credential can only run one test at a time.
func credentialBusyLocked(id string) bool {
	return candyRunning[id] != nil || fingerprintRunning[id] != nil || traceRunning[id] != nil || pelicanRunning[id]
}

type runSummary struct {
	Started   int `json:"started"`
	Busy      int `json:"busy,omitempty"`
	Skipped   int `json:"skipped,omitempty"`
	Unchecked int `json:"unchecked,omitempty"`
}

// startRuns starts a test on each selected credential that is idle. Credentials whose model list lacks
// model get skip instead. Both callbacks run with mu held; start must launch a goroutine that calls
// tasks.Done.
func startRuns(authIDs []string, all bool, catalog map[string][]string, model string, skip func(id string), start func(auth credential)) managementResponse {
	auths, err := selectedCredentials(authIDs, all)
	if err != nil {
		return jsonError(http.StatusBadGateway, err.Error())
	}
	if len(auths) == 0 {
		return jsonError(http.StatusBadRequest, "没有可测试的已启用凭证")
	}
	mu.Lock()
	defer mu.Unlock()
	if quiescing {
		return jsonError(http.StatusServiceUnavailable, "插件正在停止，请稍后重试")
	}
	summary := runSummary{}
	for _, auth := range auths {
		if credentialBusyLocked(auth.ID) {
			summary.Busy++
			continue
		}
		if known, supported := credentialSupportsModel(catalog, auth.ID, model); known && !supported {
			skip(auth.ID)
			summary.Skipped++
			continue
		} else if !known {
			summary.Unchecked++
		}
		summary.Started++
		tasks.Add(1)
		start(auth)
	}
	if summary.Skipped > 0 {
		_ = saveStateLocked()
	}
	return jsonResponse(http.StatusOK, summary)
}

func cancelCollectionResponse(scope string, body []byte) managementResponse {
	var req struct {
		AuthIDs []string `json:"auth_ids"`
		All     bool     `json:"all"`
	}
	if json.Unmarshal(body, &req) != nil {
		return jsonError(http.StatusBadRequest, "请求格式错误")
	}
	wanted := make(map[string]bool, len(req.AuthIDs))
	for _, id := range req.AuthIDs {
		wanted[id] = true
	}
	mu.Lock()
	defer mu.Unlock()
	count := 0
	cancel := func(id string, phase *string, stop context.CancelFunc) {
		if req.All || wanted[id] {
			*phase = "cancelling"
			stop()
			count++
		}
	}
	switch scope {
	case "fingerprint":
		for id, p := range fingerprintRunning {
			cancel(id, &p.Phase, p.cancel)
		}
	case "modeltrace":
		for id, p := range traceRunning {
			cancel(id, &p.Phase, p.cancel)
		}
	default:
		return jsonError(http.StatusBadRequest, "未知测试类型")
	}
	return jsonResponse(http.StatusOK, map[string]int{"cancelled": count})
}
