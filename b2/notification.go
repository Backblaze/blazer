// Copyright 2026, the Blazer authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package b2

import (
	"context"

	"github.com/Backblaze/blazer/internal/b2types"
)

// NotificationTargetType identifies a B2 notification target configuration.
type NotificationTargetType string

const (
	// NotificationTargetWebhook sends notifications to an HTTPS webhook.
	NotificationTargetWebhook NotificationTargetType = "webhook"
)

// NotificationCustomHeader is an HTTP header B2 includes in a webhook call.
type NotificationCustomHeader struct {
	Name  string
	Value string
}

// WebhookNotificationTarget configures a webhook notification target.
type WebhookNotificationTarget struct {
	URL                     string
	CustomHeaders           []NotificationCustomHeader
	HMACSHA256SigningSecret string
}

// NotificationTargetConfiguration is a tagged union. B2 currently supports
// the webhook target type, whose values are selected by TargetType.
type NotificationTargetConfiguration struct {
	TargetType NotificationTargetType
	Webhook    *WebhookNotificationTarget
}

// NotificationRule configures B2 to deliver matching bucket events to a
// target. IsSuspended and SuspensionReason are returned by B2 when applicable.
type NotificationRule struct {
	EventTypes          []string
	IsEnabled           bool
	IsSuspended         bool
	MaxEventsPerBatch   int
	Name                string
	ObjectNamePrefix    string
	SuspensionReason    string
	TargetConfiguration NotificationTargetConfiguration
}

// NotificationRules retrieves this bucket's complete notification rule set.
func (b *Bucket) NotificationRules(ctx context.Context) ([]NotificationRule, error) {
	return b.b.notificationRules(ctx)
}

// SetNotificationRules replaces this bucket's complete notification rule set.
// To add one rule, callers must first read the existing set, append the rule,
// and write the complete result.
func (b *Bucket) SetNotificationRules(ctx context.Context, rules []NotificationRule) error {
	return b.b.setNotificationRules(ctx, rules)
}

func notificationRulesFromBase(rules []b2types.EventNotificationRule) []NotificationRule {
	result := make([]NotificationRule, 0, len(rules))
	for _, rule := range rules {
		converted := NotificationRule{
			EventTypes:        append([]string(nil), rule.EventTypes...),
			IsEnabled:         rule.IsEnabled,
			IsSuspended:       rule.IsSuspended,
			MaxEventsPerBatch: rule.MaxEventsPerBatch,
			Name:              rule.Name,
			ObjectNamePrefix:  rule.ObjectNamePrefix,
			SuspensionReason:  rule.SuspensionReason,
			TargetConfiguration: NotificationTargetConfiguration{
				TargetType: NotificationTargetType(rule.TargetConfiguration.TargetType),
			},
		}
		if target := rule.TargetConfiguration.Webhook; target != nil {
			converted.TargetConfiguration.Webhook = &WebhookNotificationTarget{
				URL:                     target.URL,
				HMACSHA256SigningSecret: target.HMACSHA256SigningSecret,
			}
			for _, header := range target.CustomHeaders {
				converted.TargetConfiguration.Webhook.CustomHeaders = append(converted.TargetConfiguration.Webhook.CustomHeaders, NotificationCustomHeader{
					Name:  header.Name,
					Value: header.Value,
				})
			}
		}
		result = append(result, converted)
	}
	return result
}

func notificationRulesToBase(rules []NotificationRule) []b2types.EventNotificationRule {
	result := make([]b2types.EventNotificationRule, 0, len(rules))
	for _, rule := range rules {
		converted := b2types.EventNotificationRule{
			EventTypes:        append([]string(nil), rule.EventTypes...),
			IsEnabled:         rule.IsEnabled,
			IsSuspended:       rule.IsSuspended,
			MaxEventsPerBatch: rule.MaxEventsPerBatch,
			Name:              rule.Name,
			ObjectNamePrefix:  rule.ObjectNamePrefix,
			SuspensionReason:  rule.SuspensionReason,
			TargetConfiguration: b2types.EventNotificationTargetConfiguration{
				TargetType: string(rule.TargetConfiguration.TargetType),
			},
		}
		if target := rule.TargetConfiguration.Webhook; target != nil {
			converted.TargetConfiguration.Webhook = &b2types.WebhookNotificationTarget{
				URL:                     target.URL,
				HMACSHA256SigningSecret: target.HMACSHA256SigningSecret,
			}
			for _, header := range target.CustomHeaders {
				converted.TargetConfiguration.Webhook.CustomHeaders = append(converted.TargetConfiguration.Webhook.CustomHeaders, b2types.NotificationCustomHeader{
					Name:  header.Name,
					Value: header.Value,
				})
			}
		}
		result = append(result, converted)
	}
	return result
}
