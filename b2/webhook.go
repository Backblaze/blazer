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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// EventNotification is a B2 webhook event-notification payload.
type EventNotification struct {
	Events []Event `json:"events"`
}

// Event is one event in a B2 webhook event-notification payload.
type Event struct {
	AccountID       string `json:"accountId"`
	BucketID        string `json:"bucketId"`
	BucketName      string `json:"bucketName"`
	EventID         string `json:"eventId"`
	EventTimestamp  int64  `json:"eventTimestamp"`
	EventType       string `json:"eventType"`
	EventVersion    int    `json:"eventVersion"`
	MatchedRuleName string `json:"matchedRuleName"`
	ObjectName      string `json:"objectName"`
	ObjectSize      *int64 `json:"objectSize,omitempty"`
	ObjectVersionID string `json:"objectVersionId,omitempty"`
}

// VerifySignature verifies a B2 webhook signature for the raw request body.
func VerifySignature(body []byte, signatureHeader, signingSecret string) error {
	if !strings.HasPrefix(signatureHeader, "v1=") {
		return fmt.Errorf("webhook signature must start with v1=")
	}
	hexSignature := strings.TrimPrefix(signatureHeader, "v1=")
	if hexSignature != strings.ToLower(hexSignature) {
		return fmt.Errorf("webhook signature must use lowercase hex")
	}
	signature, err := hex.DecodeString(hexSignature)
	if err != nil {
		return fmt.Errorf("decode webhook signature: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(signingSecret))
	if _, err := mac.Write(body); err != nil {
		return fmt.Errorf("compute webhook signature: %w", err)
	}
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return fmt.Errorf("webhook signature does not match")
	}
	return nil
}

// ParseEvent parses a B2 webhook event-notification payload.
func ParseEvent(body []byte) (*EventNotification, error) {
	var notification EventNotification
	if err := json.Unmarshal(body, &notification); err != nil {
		return nil, err
	}
	return &notification, nil
}
