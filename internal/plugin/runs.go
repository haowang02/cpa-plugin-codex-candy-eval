package plugin

import (
	"context"
	"encoding/json"
	"net/http"
)

// Call with mu held; a credential can only run one test at a time.
func credentialBusyLocked(id string) bool {
	return candyRunning[id] != nil || fingerprintRunning[id] != nil || traceRunning[id] != nil
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
