package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// --- Kiro branch coverage for admin GetAvailableModels -----------------------

// kiroStubAdminService embeds stubAdminService and exposes kiro accounts on
// demand so the admin GetAccount lookup matches what GatewayService sees.
type kiroStubAdminService struct {
	*stubAdminService
	kiroAccounts []service.Account
}

func (s *kiroStubAdminService) GetAccount(_ context.Context, id int64) (*service.Account, error) {
	for i := range s.kiroAccounts {
		if s.kiroAccounts[i].ID == id {
			acc := s.kiroAccounts[i]
			return &acc, nil
		}
	}
	return s.stubAdminService.GetAccount(context.Background(), id)
}

// kiroUpstreamRecorder returns the next queued response on each Do call.
type kiroUpstreamRecorder struct {
	responses []*http.Response
}

func (r *kiroUpstreamRecorder) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return r.DoWithTLS(req, "", 0, 0, nil)
}

func (r *kiroUpstreamRecorder) DoWithTLS(_ *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	if len(r.responses) == 0 {
		return nil, errors.New("no response configured")
	}
	resp := r.responses[0]
	r.responses = r.responses[1:]
	return resp, nil
}

type kiroAdminAccountRepoStub struct {
	service.AccountRepository
	accounts []service.Account
}

func (r *kiroAdminAccountRepoStub) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	out := make([]service.Account, 0, len(r.accounts))
	for _, a := range r.accounts {
		if a.Platform == platform && a.Schedulable && a.Status == service.StatusActive {
			out = append(out, a)
		}
	}
	return out, nil
}

func (r *kiroAdminAccountRepoStub) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]service.Account, error) {
	return r.ListSchedulableByPlatform(context.Background(), platform)
}

func (r *kiroAdminAccountRepoStub) UpdateCredentials(_ context.Context, _ int64, _ map[string]any) error {
	return nil
}

// setupKiroRouter wires an AccountHandler with a real GatewayService that is
// backed by a stubbed account repo + http upstream. The kiroAccounts slice is
// used for both the admin GetAccount lookup AND the gateway's ListSchedulableByPlatform.
// Pass upstreamResp="" to simulate "Kiro management API call failed".
func setupKiroRouter(adminSvc *kiroStubAdminService, kiroAccounts []service.Account, upstreamResp string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	upstream := &kiroUpstreamRecorder{}
	if upstreamResp != "" {
		upstream.responses = append(upstream.responses, &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/x-amz-json-1.0"}},
			Body:       io.NopCloser(strings.NewReader(upstreamResp)),
		})
	}
	repo := &kiroAdminAccountRepoStub{accounts: kiroAccounts}

	// Build GatewayService with a real config + only the fields that GetKiroAvailableModels touches.
	// Other dependencies are nil; GetKiroAvailableModels never reaches them.
	gw := service.NewGatewayService(
		repo,                                // accountRepo
		nil, nil, nil, nil, nil, nil,        // groupRepo, usageLogRepo, usageBillingRepo, userRepo, userSubRepo, userGroupRateRepo
		nil,                                 // cache
		nil,                                 // cfg
		nil,                                 // schedulerSnapshot
		nil,                                 // concurrencyService
		nil,                                 // billingService
		nil,                                 // rateLimitService
		nil,                                 // billingCacheService
		nil,                                 // identityService
		upstream,                            // httpUpstream (required for Kiro ListAvailableModels)
		nil,                                 // deferredService
		nil,                                 // claudeTokenProvider
		nil,                                 // sessionLimitCache
		nil,                                 // rpmCache
		nil,                                 // digestStore
		nil,                                 // settingService
		nil,                                 // tlsFPProfileService
		nil,                                 // channelService
		nil,                                 // resolver (ModelPricingResolver)
		nil,                                 // balanceNotifyService
	)
	handler.SetGatewayService(gw)

	router.GET("/api/v1/admin/accounts/:id/models", handler.GetAvailableModels)
	return router
}

// Live Kiro management API wins over account model_mapping.
func TestAccountHandlerGetAvailableModels_KiroUsesGatewayServiceFirst(t *testing.T) {
	adminSvc := &kiroStubAdminService{stubAdminService: newStubAdminService()}
	upstreamResp := `{"defaultModel":{"modelId":"auto"},"models":[{"modelId":"claude-opus-4.8"},{"modelId":"claude-opus-4.7"}]}`
	account := service.Account{
		ID:       50,
		Name:     "kiro-oauth",
		Platform: service.PlatformKiro,
		Type:     service.AccountTypeOAuth,
		Status:      service.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token": "tok",
			"profile_arn":  "arn:test",
			"region":       "us-east-1",
			// model_mapping contains entries NOT in upstream response;
			// the live Kiro list must still win over these.
			"model_mapping": map[string]any{
				"claude-opus-4-5": "claude-opus-4.5",
				"deepseek-3.2":    "deepseek-3.2",
			},
		},
	}
	adminSvc.kiroAccounts = []service.Account{account}
	router := setupKiroRouter(adminSvc, adminSvc.kiroAccounts, upstreamResp)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/50/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 3)
	idSet := map[string]bool{}
	for _, m := range resp.Data {
		idSet[m.ID] = true
		require.Equal(t, m.ID, m.DisplayName, "DisplayName should equal ID for Kiro models")
	}
	require.True(t, idSet["auto"])
	require.True(t, idSet["claude-opus-4-7"])
	require.True(t, idSet["claude-opus-4-8"])
	require.False(t, idSet["claude-opus-4-5"], "model_mapping-only entry must NOT leak through")
	require.False(t, idSet["deepseek-3.2"], "model_mapping-only entry must NOT leak through")
}

// When live Kiro management API fails AND model_mapping is non-empty, fall
// back to model_mapping keys (NOT kiro.Models).
func TestAccountHandlerGetAvailableModels_KiroFallsBackToModelMapping(t *testing.T) {
	adminSvc := &kiroStubAdminService{stubAdminService: newStubAdminService()}
	account := service.Account{
		ID:       51,
		Name:     "kiro-oauth",
		Platform: service.PlatformKiro,
		Type:     service.AccountTypeOAuth,
		Status:      service.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token": "tok",
			"profile_arn":  "arn:test",
			"region":       "us-east-1",
			"model_mapping": map[string]any{
				"glm-5":            "glm-5",
				"deepseek-3.2":     "deepseek-3.2",
				"qwen3-coder-next": "qwen3-coder-next",
			},
		},
	}
	adminSvc.kiroAccounts = []service.Account{account}
	router := setupKiroRouter(adminSvc, adminSvc.kiroAccounts, "")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/51/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	idSet := map[string]bool{}
	for _, m := range resp.Data {
		idSet[m.ID] = true
	}
	require.True(t, idSet["glm-5"])
	require.True(t, idSet["deepseek-3.2"])
	require.True(t, idSet["qwen3-coder-next"])
	// Kiro model_mapping is expanded with default mappings, so we should see
	// at least the 3 explicit keys plus more from kiro.ModelMapping.
	require.GreaterOrEqual(t, len(resp.Data), 10, "kiro model_mapping fallback should include defaults")
}

// When live Kiro management API fails AND model_mapping is empty, fall back to
// kiro.Models constant (the full 20-entry default list).
func TestAccountHandlerGetAvailableModels_KiroFallsBackToKiroConstants(t *testing.T) {
	adminSvc := &kiroStubAdminService{stubAdminService: newStubAdminService()}
	account := service.Account{
		ID:       52,
		Name:     "kiro-oauth",
		Platform: service.PlatformKiro,
		Type:     service.AccountTypeOAuth,
		Status:      service.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token": "tok",
			"profile_arn":  "arn:test",
			"region":       "us-east-1",
		},
	}
	adminSvc.kiroAccounts = []service.Account{account}
	router := setupKiroRouter(adminSvc, adminSvc.kiroAccounts, "")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/52/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.GreaterOrEqual(t, len(resp.Data), 10, "kiro.Models fallback should expose the full default model list")
	idSet := map[string]bool{}
	for _, m := range resp.Data {
		idSet[m.ID] = true
	}
	require.True(t, idSet["claude-opus-4-8"])
	require.True(t, idSet["claude-opus-4-7"])
	require.True(t, idSet["claude-sonnet-5"])
	require.True(t, idSet["deepseek-3.2"])
	require.True(t, idSet["qwen3-coder-next"])
}
