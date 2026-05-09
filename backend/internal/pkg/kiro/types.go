package kiro

import (
	"encoding/json"
	"strings"
	"time"
)

type Credentials struct {
	UUID                  string `json:"uuid,omitempty"`
	AccessToken           string `json:"access_token,omitempty"`
	RefreshToken          string `json:"refresh_token,omitempty"`
	ClientID              string `json:"client_id,omitempty"`
	ClientSecret          string `json:"client_secret,omitempty"`
	ClientSecretExpiresAt int64  `json:"client_secret_expires_at,omitempty"`
	AuthMethod            string `json:"auth_method,omitempty"`
	ExpiresAt             string `json:"expires_at,omitempty"`
	ProfileARN            string `json:"profile_arn,omitempty"`
	Region                string `json:"region,omitempty"`
	Provider              string `json:"provider,omitempty"`
	StartURL              string `json:"start_url,omitempty"`
	SSORegion             string `json:"sso_region,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

type Request struct {
	Model       string          `json:"model"`
	System      json.RawMessage `json:"system,omitempty"`
	Messages    []Message       `json:"messages"`
	Tools       []Tool          `json:"tools,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type Response struct {
	Content    string `json:"content,omitempty"`
	StopReason string `json:"stop_reason,omitempty"`
	Usage      Usage  `json:"usage,omitempty"`
}

type StreamEvent struct {
	Type       string
	Content    string
	ToolUse    *ToolUse
	Input      string
	Stop       bool
	Percentage float64
}

type ToolUse struct {
	ToolUseID string
	Name      string
	Input     string
	Stop      bool
}

type UsageLimits struct {
	DaysUntilReset int                        `json:"days_until_reset"`
	NextDateReset  *time.Time                 `json:"next_date_reset,omitempty"`
	Subscription   *UsageSubscription         `json:"subscription,omitempty"`
	User           *UsageUser                 `json:"user,omitempty"`
	Breakdown      []UsageBreakdownItem       `json:"usage_breakdown,omitempty"`
	Raw            map[string]json.RawMessage `json:"-"`
}

type UsageSubscription struct {
	Title             string `json:"title,omitempty"`
	Type              string `json:"type,omitempty"`
	AccountType       string `json:"account_type,omitempty"`
	UpgradeCapability string `json:"upgrade_capability,omitempty"`
	OverageCapability string `json:"overage_capability,omitempty"`
}

type UsageUser struct {
	Email  string `json:"email,omitempty"`
	UserID string `json:"user_id,omitempty"`
}

type UsageBonusInfo struct {
	Code         string     `json:"code,omitempty"`
	DisplayName  string     `json:"display_name,omitempty"`
	Description  string     `json:"description,omitempty"`
	Status       string     `json:"status,omitempty"`
	CurrentUsage float64    `json:"current_usage"`
	UsageLimit   float64    `json:"usage_limit"`
	RedeemedAt   *time.Time `json:"redeemed_at,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

type UsageFreeTrialInfo struct {
	Status       string     `json:"status,omitempty"`
	CurrentUsage float64    `json:"current_usage"`
	UsageLimit   float64    `json:"usage_limit"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

type UsageBreakdownItem struct {
	ResourceType      string              `json:"resource_type,omitempty"`
	DisplayName       string              `json:"display_name,omitempty"`
	DisplayNamePlural string              `json:"display_name_plural,omitempty"`
	Unit              string              `json:"unit,omitempty"`
	Currency          string              `json:"currency,omitempty"`
	CurrentUsage      float64             `json:"current_usage"`
	UsageLimit        float64             `json:"usage_limit"`
	CurrentOverages   float64             `json:"current_overages"`
	OverageCap        float64             `json:"overage_cap"`
	OverageRate       float64             `json:"overage_rate"`
	OverageCharges    float64             `json:"overage_charges"`
	NextDateReset     *time.Time          `json:"next_date_reset,omitempty"`
	FreeTrial         *UsageFreeTrialInfo `json:"free_trial,omitempty"`
	Bonuses           []UsageBonusInfo    `json:"bonuses,omitempty"`
	TotalUsed         float64             `json:"total_used"`
	TotalLimit        float64             `json:"total_limit"`
	TotalPercent      int                 `json:"total_percent"`
}

func FormatUsageLimits(raw []byte) (*UsageLimits, error) {
	var data usageLimitsRaw
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	var rawMap map[string]json.RawMessage
	_ = json.Unmarshal(raw, &rawMap)

	out := &UsageLimits{
		DaysUntilReset: data.DaysUntilReset,
		NextDateReset:  unixSecondsPtr(data.NextDateReset),
		Breakdown:      make([]UsageBreakdownItem, 0, len(data.UsageBreakdownList)),
		Raw:            rawMap,
	}
	if data.SubscriptionInfo != nil {
		out.Subscription = &UsageSubscription{
			Title:             data.SubscriptionInfo.SubscriptionTitle,
			Type:              data.SubscriptionInfo.Type,
			AccountType:       MapSubscriptionTypeToAccountType(data.SubscriptionInfo.Type),
			UpgradeCapability: data.SubscriptionInfo.UpgradeCapability,
			OverageCapability: data.SubscriptionInfo.OverageCapability,
		}
	}
	if data.UserInfo != nil {
		out.User = &UsageUser{Email: data.UserInfo.Email, UserID: data.UserInfo.UserID}
	}

	for _, item := range data.UsageBreakdownList {
		breakdown := UsageBreakdownItem{
			ResourceType:      item.ResourceType,
			DisplayName:       item.DisplayName,
			DisplayNamePlural: item.DisplayNamePlural,
			Unit:              item.Unit,
			Currency:          item.Currency,
			CurrentUsage:      preferFloat(item.CurrentUsageWithPrecision, item.CurrentUsage),
			UsageLimit:        preferFloat(item.UsageLimitWithPrecision, item.UsageLimit),
			CurrentOverages:   preferFloat(item.CurrentOveragesWithPrecision, item.CurrentOverages),
			OverageCap:        preferFloat(item.OverageCapWithPrecision, item.OverageCap),
			OverageRate:       item.OverageRate,
			OverageCharges:    item.OverageCharges,
			NextDateReset:     unixSecondsPtr(item.NextDateReset),
			Bonuses:           make([]UsageBonusInfo, 0, len(item.Bonuses)),
		}
		breakdown.TotalUsed = breakdown.CurrentUsage
		breakdown.TotalLimit = breakdown.UsageLimit

		if item.FreeTrialInfo != nil {
			breakdown.FreeTrial = &UsageFreeTrialInfo{
				Status:       item.FreeTrialInfo.FreeTrialStatus,
				CurrentUsage: preferFloat(item.FreeTrialInfo.CurrentUsageWithPrecision, item.FreeTrialInfo.CurrentUsage),
				UsageLimit:   preferFloat(item.FreeTrialInfo.UsageLimitWithPrecision, item.FreeTrialInfo.UsageLimit),
				ExpiresAt:    unixSecondsPtr(item.FreeTrialInfo.FreeTrialExpiry),
			}
			breakdown.TotalUsed += breakdown.FreeTrial.CurrentUsage
			breakdown.TotalLimit += breakdown.FreeTrial.UsageLimit
		}

		for _, bonus := range item.Bonuses {
			formatted := UsageBonusInfo{
				Code:         bonus.BonusCode,
				DisplayName:  bonus.DisplayName,
				Description:  bonus.Description,
				Status:       bonus.Status,
				CurrentUsage: bonus.CurrentUsage,
				UsageLimit:   bonus.UsageLimit,
				RedeemedAt:   unixSecondsPtr(bonus.RedeemedAt),
				ExpiresAt:    unixSecondsPtr(bonus.ExpiresAt),
			}
			breakdown.Bonuses = append(breakdown.Bonuses, formatted)
			if formatted.Status == "ACTIVE" || formatted.Status == "REDEEMED" {
				breakdown.TotalUsed += formatted.CurrentUsage
				breakdown.TotalLimit += formatted.UsageLimit
			}
		}

		if breakdown.TotalLimit > 0 {
			breakdown.TotalPercent = int((breakdown.TotalUsed/breakdown.TotalLimit)*100 + 0.5)
		}
		out.Breakdown = append(out.Breakdown, breakdown)
	}

	return out, nil
}

func MapSubscriptionTypeToAccountType(subscriptionType string) string {
	normalized := strings.ToUpper(strings.TrimSpace(subscriptionType))
	switch {
	case normalized == "":
		return "UNKNOWN"
	case normalized == "FREE_TIER" || normalized == "FREE" || strings.Contains(normalized, "FREE"):
		return "FREE"
	case normalized == "PRO" || normalized == "PRO_TIER" || strings.Contains(normalized, "PRO"):
		return "PRO"
	default:
		return "UNKNOWN"
	}
}

type usageLimitsRaw struct {
	DaysUntilReset     int                   `json:"daysUntilReset"`
	NextDateReset      float64               `json:"nextDateReset"`
	SubscriptionInfo   *usageSubscriptionRaw `json:"subscriptionInfo"`
	UserInfo           *usageUserRaw         `json:"userInfo"`
	UsageBreakdownList []usageBreakdownRaw   `json:"usageBreakdownList"`
}

type usageSubscriptionRaw struct {
	SubscriptionTitle string `json:"subscriptionTitle"`
	Type              string `json:"type"`
	UpgradeCapability string `json:"upgradeCapability"`
	OverageCapability string `json:"overageCapability"`
}

type usageUserRaw struct {
	Email  string `json:"email"`
	UserID string `json:"userId"`
}

type usageBreakdownRaw struct {
	ResourceType                 string             `json:"resourceType"`
	DisplayName                  string             `json:"displayName"`
	DisplayNamePlural            string             `json:"displayNamePlural"`
	Unit                         string             `json:"unit"`
	Currency                     string             `json:"currency"`
	CurrentUsage                 float64            `json:"currentUsage"`
	CurrentUsageWithPrecision    *float64           `json:"currentUsageWithPrecision"`
	UsageLimit                   float64            `json:"usageLimit"`
	UsageLimitWithPrecision      *float64           `json:"usageLimitWithPrecision"`
	CurrentOverages              float64            `json:"currentOverages"`
	CurrentOveragesWithPrecision *float64           `json:"currentOveragesWithPrecision"`
	OverageCap                   float64            `json:"overageCap"`
	OverageCapWithPrecision      *float64           `json:"overageCapWithPrecision"`
	OverageRate                  float64            `json:"overageRate"`
	OverageCharges               float64            `json:"overageCharges"`
	NextDateReset                float64            `json:"nextDateReset"`
	FreeTrialInfo                *usageFreeTrialRaw `json:"freeTrialInfo"`
	Bonuses                      []usageBonusRaw    `json:"bonuses"`
}

type usageFreeTrialRaw struct {
	FreeTrialStatus           string   `json:"freeTrialStatus"`
	CurrentUsage              float64  `json:"currentUsage"`
	CurrentUsageWithPrecision *float64 `json:"currentUsageWithPrecision"`
	UsageLimit                float64  `json:"usageLimit"`
	UsageLimitWithPrecision   *float64 `json:"usageLimitWithPrecision"`
	FreeTrialExpiry           float64  `json:"freeTrialExpiry"`
}

type usageBonusRaw struct {
	BonusCode    string  `json:"bonusCode"`
	DisplayName  string  `json:"displayName"`
	Description  string  `json:"description"`
	Status       string  `json:"status"`
	CurrentUsage float64 `json:"currentUsage"`
	UsageLimit   float64 `json:"usageLimit"`
	RedeemedAt   float64 `json:"redeemedAt"`
	ExpiresAt    float64 `json:"expiresAt"`
}

func preferFloat(preferred *float64, fallback float64) float64 {
	if preferred != nil {
		return *preferred
	}
	return fallback
}

func unixSecondsPtr(sec float64) *time.Time {
	if sec <= 0 {
		return nil
	}
	t := time.Unix(int64(sec), 0).UTC()
	return &t
}
