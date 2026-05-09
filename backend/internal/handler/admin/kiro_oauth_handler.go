package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type KiroOAuthHandler struct {
	kiroOAuthService *service.KiroOAuthService
}

func NewKiroOAuthHandler(kiroOAuthService *service.KiroOAuthService) *KiroOAuthHandler {
	return &KiroOAuthHandler{kiroOAuthService: kiroOAuthService}
}

type KiroStartDeviceAuthRequest struct {
	AuthType string `json:"auth_type"`
	Region   string `json:"region"`
	StartURL string `json:"start_url"`
	ProxyID  *int64 `json:"proxy_id"`
}

type KiroStartSocialAuthRequest struct {
	Provider    string `json:"provider"`
	Region      string `json:"region"`
	RedirectURI string `json:"redirect_uri"`
	ProxyID     *int64 `json:"proxy_id"`
}

type KiroCompleteSocialAuthRequest struct {
	SessionID      string `json:"session_id"`
	CallbackOrCode string `json:"callback_or_code"`
}

func (h *KiroOAuthHandler) StartDeviceAuth(c *gin.Context) {
	var req KiroStartDeviceAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求无效: "+err.Error())
		return
	}
	result, err := h.kiroOAuthService.StartDeviceAuth(c.Request.Context(), service.KiroDeviceAuthInput{
		AuthType: req.AuthType,
		Region:   req.Region,
		StartURL: req.StartURL,
		ProxyID:  req.ProxyID,
	})
	if err != nil {
		response.BadRequest(c, "启动 Kiro 授权失败: "+err.Error())
		return
	}
	response.Success(c, result)
}

func (h *KiroOAuthHandler) GetSession(c *gin.Context) {
	result, err := h.kiroOAuthService.GetSessionStatus(c.Request.Context(), c.Param("session_id"))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, result)
}

func (h *KiroOAuthHandler) StartSocialAuth(c *gin.Context) {
	var req KiroStartSocialAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求无效: "+err.Error())
		return
	}
	result, err := h.kiroOAuthService.StartSocialAuth(c.Request.Context(), service.KiroSocialAuthInput{
		Provider:    req.Provider,
		Region:      req.Region,
		RedirectURI: req.RedirectURI,
		ProxyID:     req.ProxyID,
	})
	if err != nil {
		response.BadRequest(c, "启动 Kiro Social 授权失败: "+err.Error())
		return
	}
	response.Success(c, result)
}

func (h *KiroOAuthHandler) CompleteSocialAuth(c *gin.Context) {
	var req KiroCompleteSocialAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求无效: "+err.Error())
		return
	}
	result, err := h.kiroOAuthService.CompleteSocialAuth(c.Request.Context(), service.KiroCompleteSocialInput{
		SessionID:      req.SessionID,
		CallbackOrCode: req.CallbackOrCode,
	})
	if err != nil {
		response.BadRequest(c, "完成 Kiro Social 授权失败: "+err.Error())
		return
	}
	response.Success(c, result)
}

func (h *KiroOAuthHandler) CancelSession(c *gin.Context) {
	if err := h.kiroOAuthService.CancelSession(c.Param("session_id")); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, gin.H{"cancelled": true})
}

func (h *KiroOAuthHandler) ScanTokens(c *gin.Context) {
	response.Success(c, h.kiroOAuthService.ScanTokens())
}
