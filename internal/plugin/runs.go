package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
)

// progress is the test a credential is running; a credential runs one test at a time.
type progress struct {
	Test       string `json:"test"`
	Model      string `json:"model"`
	Done       int    `json:"done"`
	Total      int    `json:"total"`
	Cancelling bool   `json:"cancelling,omitempty"`
	cancel     context.CancelFunc
}

// running maps credential IDs to the test they run, guarded by mu.
var running = map[string]*progress{}

// runRequest selects the credentials a test runs on and its model. ModelCatalog lists the models of the
// credentials whose model list the page could read.
type runRequest struct {
	CredentialIDs []string            `json:"credential_ids"`
	All           bool                `json:"all"`
	Model         string              `json:"model"`
	ModelCatalog  map[string][]string `json:"model_catalog,omitempty"`
}

type runSummary struct {
	Started   int `json:"started"`
	Busy      int `json:"busy,omitempty"`
	Skipped   int `json:"skipped,omitempty"`
	Unchecked int `json:"unchecked,omitempty"`
}

// startRuns starts test, which takes total steps, on each selected credential that is idle. Credentials
// whose model list lacks the model get skip instead, which runs with mu held. run goes in a goroutine of its
// own, with a context that ends when the run is cancelled or the plugin quiesces.
func startRuns(test string, req runRequest, total int, skip func(id string), run func(ctx context.Context, auth credential, p *progress)) managementResponse {
	auths, err := selectedCredentials(req.CredentialIDs, req.All)
	if err != nil {
		return jsonError(http.StatusBadGateway, err.Error())
	}
	return startCredentialRuns(test, req, auths, total, skip, run)
}

func startCredentialRuns(test string, req runRequest, auths []credential, total int, skip func(string), run func(context.Context, credential, *progress)) managementResponse {
	if len(auths) == 0 {
		return jsonError(http.StatusBadRequest, "没有可测试的已启用凭证")
	}
	mu.Lock()
	defer mu.Unlock()
	if runCtx.Err() != nil {
		return jsonError(http.StatusServiceUnavailable, "插件正在停止，请稍后重试")
	}
	summary := runSummary{}
	for _, auth := range auths {
		if running[auth.ID] != nil {
			summary.Busy++
			continue
		}
		if known, supported := credentialSupportsModel(req.ModelCatalog, auth.ID, req.Model); known && !supported {
			skip(auth.ID)
			summary.Skipped++
			continue
		} else if !known {
			summary.Unchecked++
		}
		summary.Started++
		ctx, cancel := context.WithCancel(runCtx)
		p := &progress{Test: test, Model: req.Model, Total: total, cancel: cancel}
		running[auth.ID] = p
		tasks.Add(1)
		go func() {
			defer tasks.Done()
			defer func() {
				cancel()
				mu.Lock()
				delete(running, auth.ID)
				mu.Unlock()
			}()
			run(ctx, auth, p)
		}()
	}
	if summary.Skipped > 0 {
		_ = saveStateLocked()
	}
	return jsonResponse(http.StatusOK, summary)
}

// cancelResponse stops the tests running on the credentials in body: they send no new requests and abandon
// those in flight.
func cancelResponse(body []byte) managementResponse {
	var req struct {
		CredentialIDs []string `json:"credential_ids"`
	}
	if json.Unmarshal(body, &req) != nil {
		return jsonError(http.StatusBadRequest, "请求格式错误")
	}
	mu.Lock()
	defer mu.Unlock()
	count := 0
	for id, p := range running {
		if slices.Contains(req.CredentialIDs, id) {
			p.Cancelling = true
			p.cancel()
			count++
		}
	}
	return jsonResponse(http.StatusOK, map[string]int{"cancelled": count})
}
