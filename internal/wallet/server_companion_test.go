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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eudi-dev/eudi-dev/v3/internal/mock"
	"github.com/eudi-dev/eudi-dev/v3/internal/oid4vp"
	"github.com/eudi-dev/eudi-dev/v3/internal/sdjwt"
)

func companionRequest(t *testing.T, origin string, protocol string, data any) *http.Request {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"origin":  origin,
		"digital": map[string]any{"requests": []any{map[string]any{"protocol": protocol, "data": data}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/companion/dc-api", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "localhost:8085"
	return req
}

func unsignedPIDRequest() map[string]any {
	return map[string]any{
		"response_type": "vp_token",
		"response_mode": "dc_api",
		"nonce":         "companion-nonce",
		"dcql_query": map[string]any{"credentials": []any{map[string]any{
			"id": "pid", "format": "dc+sd-jwt",
			"meta":   map[string]any{"vct_values": []any{mock.DefaultPIDVCT}},
			"claims": []any{map[string]any{"path": []any{"given_name"}}},
		}}},
	}
}

// OpenID4VP 1.0 Appendix A.4 makes the origin the audience, so the origin the
// bridge page relays must reach the Key Binding JWT.
func TestCompanionPresentationAPI_UsesTheRelayedOrigin(t *testing.T) {
	srv := newTestServer(t, true)

	req := companionRequest(t, "https://rp.example", "openid4vp-v1-unsigned", unsignedPIDRequest())
	req.Header.Set("Origin", "http://localhost:8085")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	data, _ := decodeJSON(t, rec)["data"].(map[string]any)
	vpToken, _ := data["vp_token"].(map[string]any)
	entries, _ := vpToken["pid"].([]any)
	if len(entries) != 1 {
		t.Fatalf("vp_token.pid = %v, want one presentation", vpToken["pid"])
	}
	token, _ := entries[0].(string)
	parsed, err := sdjwt.Parse(token)
	if err != nil {
		t.Fatalf("parsing VP token: %v", err)
	}
	if parsed.KeyBindingJWT == nil || parsed.KeyBindingJWT.Payload["aud"] != "origin:https://rp.example" {
		t.Fatalf("Key Binding JWT audience is not origin:https://rp.example: %+v", parsed.KeyBindingJWT)
	}
}

// Appendix A.2: "If the Origin does not match any of the entries in
// expected_origins, the Wallet MUST return an error." The relayed origin is
// held to that as the platform's is.
func TestCompanionPresentationAPI_RefusesAnOriginOutsideExpectedOrigins(t *testing.T) {
	srv := newTestServer(t, true)
	srv.wallet.ValidationMode = ValidationModeStrict
	key, chain, _ := registeredVerifier(t, srv.wallet)
	requestObject, err := oid4vp.SignRequestObjectJWT(map[string]any{
		"client_id": oid4vp.X509HashClientID(chain[0]), "response_type": "vp_token", "response_mode": "dc_api",
		"nonce": "n", "expected_origins": []any{"https://rp.example"},
		"dcql_query": unsignedPIDRequest()["dcql_query"],
	}, key, chain)
	if err != nil {
		t.Fatal(err)
	}

	req := companionRequest(t, "https://other.example", "openid4vp-v1-signed", map[string]any{"request": requestObject})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "expected_origins") {
		t.Fatalf("got %d: %s, want 400 naming expected_origins", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "vp_token") {
		t.Errorf("a refused request produced a presentation: %s", rec.Body.String())
	}
}

func TestCompanionPresentationAPI_RefusesAnOriginThatIsNotSerialized(t *testing.T) {
	srv := newTestServer(t, true)
	for _, origin := range []string{"", "rp.example", "https://rp.example/", "https://rp.example/path", "https://user@rp.example", "ftp://rp.example", "null"} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, companionRequest(t, origin, "openid4vp-v1-unsigned", unsignedPIDRequest()))
		if rec.Code != http.StatusBadRequest || strings.Contains(rec.Body.String(), "vp_token") {
			t.Errorf("origin %q: got %d: %s, want 400", origin, rec.Code, rec.Body.String())
		}
	}
}

// Unlike /api/dc-api, this endpoint takes the origin from its body, so a
// foreign page must not reach it.
func TestCompanionPresentationAPI_RefusesACrossOriginCaller(t *testing.T) {
	srv := newTestServer(t, true)

	req := companionRequest(t, "https://rp.example", "openid4vp-v1-unsigned", unsignedPIDRequest())
	req.Header.Set("Origin", "https://rp.example")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
}

func TestCompanionBridgePageIsServed(t *testing.T) {
	srv := newTestServer(t, true)
	for _, path := range []string{"/companion.html", "/companion.js"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Host = "localhost:8085"
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
		}
	}
}
