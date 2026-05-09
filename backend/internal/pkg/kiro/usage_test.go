package kiro

import "testing"

func TestFormatUsageLimitsTotalsPrecisionAndSubscription(t *testing.T) {
	raw := []byte(`{
		"daysUntilReset": 3,
		"nextDateReset": 1893456000,
		"subscriptionInfo": {
			"subscriptionTitle": "Kiro Pro",
			"type": "PRO_TIER",
			"upgradeCapability": "AVAILABLE",
			"overageCapability": "ENABLED"
		},
		"userInfo": {
			"email": "user@example.com",
			"userId": "u-1"
		},
		"usageBreakdownList": [{
			"resourceType": "AGENTIC_REQUEST",
			"displayName": "Credit",
			"displayNamePlural": "Credits",
			"unit": "credit",
			"currency": "USD",
			"currentUsage": 1,
			"currentUsageWithPrecision": 1.25,
			"usageLimit": 10,
			"usageLimitWithPrecision": 10.5,
			"currentOverages": 0,
			"overageCap": 100,
			"overageRate": 0.04,
			"overageCharges": 0,
			"nextDateReset": 1893456000,
			"freeTrialInfo": {
				"freeTrialStatus": "ACTIVE",
				"currentUsage": 2,
				"usageLimit": 5,
				"freeTrialExpiry": 1893542400
			},
			"bonuses": [{
				"bonusCode": "WELCOME",
				"displayName": "Welcome",
				"description": "Welcome credits",
				"status": "ACTIVE",
				"currentUsage": 0.5,
				"usageLimit": 2,
				"redeemedAt": 1893369600,
				"expiresAt": 1893542400
			}, {
				"bonusCode": "OLD",
				"status": "EXPIRED",
				"currentUsage": 99,
				"usageLimit": 99
			}]
		}]
	}`)

	usage, err := FormatUsageLimits(raw)
	if err != nil {
		t.Fatalf("FormatUsageLimits error: %v", err)
	}
	if usage.Subscription == nil || usage.Subscription.AccountType != "PRO" {
		t.Fatalf("subscription account type = %#v, want PRO", usage.Subscription)
	}
	if usage.User == nil || usage.User.Email != "user@example.com" {
		t.Fatalf("user = %#v, want email", usage.User)
	}
	if len(usage.Breakdown) != 1 {
		t.Fatalf("breakdown len = %d, want 1", len(usage.Breakdown))
	}
	item := usage.Breakdown[0]
	if item.CurrentUsage != 1.25 || item.UsageLimit != 10.5 {
		t.Fatalf("precision values = %v/%v, want 1.25/10.5", item.CurrentUsage, item.UsageLimit)
	}
	if item.TotalUsed != 3.75 || item.TotalLimit != 17.5 {
		t.Fatalf("totals = %v/%v, want 3.75/17.5", item.TotalUsed, item.TotalLimit)
	}
	if item.TotalPercent != 21 {
		t.Fatalf("total percent = %d, want 21", item.TotalPercent)
	}
}
