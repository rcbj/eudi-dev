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

package wallet

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/eudi-dev/eudi-dev/v3/internal/apierror"
	"github.com/eudi-dev/eudi-dev/v3/internal/oid4vp"
)

// companionPresentationRequest is what companion.html relays for the Wallet
// Companion browser extension: the navigator.credentials.get() argument and
// the verifier page's origin. The extension stands in for the platform in a
// browser without the Digital Credentials API. OpenID4VP 1.0 Appendix A.2
// has the platform report the origin, and the page learns it from the
// browser-set MessageEvent.origin of the extension's message.
type companionPresentationRequest struct {
	Origin string `json:"origin"`
	oid4vp.BrowserAPIRequestEnvelope
}

// The guard keeps this path same-origin, unlike /api/dc-api, so a foreign
// page cannot name an origin other than its own.
func (s *Server) handleCompanionPresentationAPI(w http.ResponseWriter, r *http.Request) {
	var body companionPresentationRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierror.InvalidJSON(w, err)
		return
	}
	if err := checkSerializedOrigin(body.Origin); err != nil {
		apierror.WriteErr(w, http.StatusBadRequest, apierror.InvalidRequest, err)
		return
	}
	s.answerBrowserPresentation(w, r, body.BrowserAPIRequestEnvelope, body.Origin)
}

// expected_origins holds serialized origins (RFC 6454 §6.2), and the wallet
// compares them as strings, so only that form can match.
func checkSerializedOrigin(origin string) error {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" ||
		u.Scheme+"://"+u.Host != origin {
		return fmt.Errorf("origin must be a serialized http or https origin, got %q", origin)
	}
	return nil
}
