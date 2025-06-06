// Copyright 2024 The Sigstore Authors.
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

package notifications

import (
	"context"
	"testing"

	"github.com/sigstore/rekor-monitor/pkg/identity"
)

func TestMailgunSendFailure(t *testing.T) {
	mailgunNotificationInput := MailgunNotificationInput{
		RecipientEmailAddress: "test-recipient@example.com",
		SenderEmailAddress:    "test-sender@example.com",
		MailgunAPIKey:         "",
		MailgunDomainName:     "",
	}
	monitoredIdentity := identity.MonitoredIdentity{
		Identity: "test-identity",
		FoundIdentityEntries: []identity.LogEntry{
			{
				CertSubject: "test-cert-subject",
				UUID:        "test-uuid",
				Index:       0,
			},
		},
	}

	err := mailgunNotificationInput.Send(context.Background(), []identity.MonitoredIdentity{monitoredIdentity})
	if err == nil {
		t.Errorf("expected error, received nil")
	}
}
