package controller

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type channelOAuthTransport func(*http.Request) (*http.Response, error)

func (transport channelOAuthTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func channelOAuthRequest(identity service.AuthIdentity, body string, cookies []*http.Cookie, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/channel/codex/oauth", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		c.Request.AddCookie(cookie)
	}
	c.Set("id", identity.UserID)
	c.Set("session_id", identity.SessionID)
	c.Set("auth_version", identity.UserAuthVersion)
	c.Set("session_version", identity.SessionVersion)
	handler(c)
	return response
}

func TestCodexChannelOAuthUsesDashboardSessionWithoutLegacySessionMiddleware(t *testing.T) {
	_, identity := setupSecurityEnrollmentTest(t)
	var started *httptest.ResponseRecorder
	require.NotPanics(t, func() { started = channelOAuthRequest(identity, "", nil, StartCodexOAuth) })
	require.NotNil(t, started)
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			AuthorizeURL string `json:"authorize_url"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(started.Body.Bytes(), &payload))
	require.True(t, payload.Success, started.Body.String())
	authorizeURL, err := url.Parse(payload.Data.AuthorizeURL)
	require.NoError(t, err)
	state := authorizeURL.Query().Get("state")
	require.NotEmpty(t, state)
	assert.Equal(t, "S256", authorizeURL.Query().Get("code_challenge_method"))
	cookies := started.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.True(t, cookies[0].HttpOnly)
	assert.Equal(t, http.SameSiteStrictMode, cookies[0].SameSite)
	assert.NotContains(t, started.Body.String(), "verifier")

	service.InitHttpClient()
	client := service.GetHttpClient()
	previousTransport := client.Transport
	t.Cleanup(func() { client.Transport = previousTransport })
	requests := 0
	client.Transport = channelOAuthTransport(func(request *http.Request) (*http.Response, error) {
		requests++
		assert.Equal(t, "auth.openai.com", request.URL.Host)
		require.NoError(t, request.ParseForm())
		assert.Equal(t, "test-code", request.Form.Get("code"))
		assert.NotEmpty(t, request.Form.Get("code_verifier"))
		challenge := sha256.Sum256([]byte(request.Form.Get("code_verifier")))
		assert.Equal(t, authorizeURL.Query().Get("code_challenge"), base64.RawURLEncoding.EncodeToString(challenge[:]))
		claims := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"test-account"},"email":"user@example.com"}`))
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"access_token":"header.` + claims + `.signature","refresh_token":"test-refresh","expires_in":3600}`))}, nil
	})
	body, err := common.Marshal(gin.H{"input": "test-code#" + state})
	require.NoError(t, err)
	completed := channelOAuthRequest(identity, string(body), cookies, CompleteCodexOAuth)
	assert.Contains(t, completed.Body.String(), `"success":true`)
	assert.Contains(t, completed.Body.String(), `test-account`)
	replayed := channelOAuthRequest(identity, string(body), cookies, CompleteCodexOAuth)
	assert.Contains(t, replayed.Body.String(), `"success":false`)
	assert.Equal(t, 1, requests)
}

func TestGitHubCopilotChannelOAuthKeepsPendingFlowAndConsumesSuccessfulAuthorization(t *testing.T) {
	_, identity := setupSecurityEnrollmentTest(t)
	service.InitHttpClient()
	client := service.GetHttpClient()
	previousTransport := client.Transport
	t.Cleanup(func() { client.Transport = previousTransport })
	pending := true
	client.Transport = channelOAuthTransport(func(request *http.Request) (*http.Response, error) {
		assert.Equal(t, "github.com", request.URL.Host)
		body := `{"device_code":"private-device-code","user_code":"PUBLIC-CODE","verification_uri":"https://github.com/login/device","expires_in":900,"interval":1}`
		if request.URL.Path == "/login/oauth/access_token" {
			require.NoError(t, request.ParseForm())
			assert.Equal(t, "private-device-code", request.Form.Get("device_code"))
			body = `{"access_token":"authorized-github-token"}`
			if pending {
				body = `{"error":"authorization_pending"}`
			}
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	started := channelOAuthRequest(identity, "", nil, StartGitHubCopilotOAuth)
	require.Contains(t, started.Body.String(), `"success":true`)
	assert.Contains(t, started.Body.String(), `PUBLIC-CODE`)
	assert.NotContains(t, started.Body.String(), `private-device-code`)
	cookies := started.Result().Cookies()
	require.Len(t, cookies, 1)
	waiting := channelOAuthRequest(identity, "", cookies, func(c *gin.Context) {
		ctx, cancel := context.WithCancel(c.Request.Context())
		cancel()
		c.Request = c.Request.WithContext(ctx)
		CompleteGitHubCopilotOAuth(c)
	})
	assert.Contains(t, waiting.Body.String(), `authorization pending`)
	pending = false
	completed := channelOAuthRequest(identity, "", cookies, CompleteGitHubCopilotOAuth)
	assert.Contains(t, completed.Body.String(), `"success":true`)
	assert.Contains(t, completed.Body.String(), `authorized-github-token`)
	replayed := channelOAuthRequest(identity, "", cookies, CompleteGitHubCopilotOAuth)
	assert.Contains(t, replayed.Body.String(), `"success":false`)
}

func TestCodexChannelOAuthRejectsExpiredOrDifferentSessionFlows(t *testing.T) {
	for _, scenario := range []string{"expired", "different session", "revoked", "wrong state", "missing cookie"} {
		t.Run(scenario, func(t *testing.T) {
			_, identity := setupSecurityEnrollmentTest(t)
			started := channelOAuthRequest(identity, "", nil, StartCodexOAuth)
			var payload struct {
				Data struct {
					AuthorizeURL string `json:"authorize_url"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(started.Body.Bytes(), &payload))
			u, err := url.Parse(payload.Data.AuthorizeURL)
			require.NoError(t, err)
			state := u.Query().Get("state")
			cookies := started.Result().Cookies()
			switch scenario {
			case "expired":
				require.NoError(t, model.DB.Model(&model.AuthFlow{}).Where("user_id = ?", identity.UserID).Update("expires_at", time.Now().Add(-time.Minute)).Error)
			case "different session":
				identity.SessionID = "another-session"
			case "revoked":
				_, err := model.RevokeUserSession(identity.UserID, identity.SessionID, "test")
				require.NoError(t, err)
			case "wrong state":
				state = "wrong-state"
			case "missing cookie":
				cookies = nil
			}
			body, err := common.Marshal(gin.H{"input": "test-code#" + state})
			require.NoError(t, err)
			result := channelOAuthRequest(identity, string(body), cookies, CompleteCodexOAuth)
			assert.Contains(t, result.Body.String(), `"success":false`)
		})
	}
}

func TestChannelOAuthCredentialCommitRejectsChangedChannelAndKeepsFlowRetryable(t *testing.T) {
	_, identity := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Channel{}))
	channel := model.Channel{Type: constant.ChannelTypeCodex, Name: "authorized-channel", Key: "original-key"}
	require.NoError(t, model.DB.Create(&channel).Error)
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/channel/1/codex/oauth", nil)
	token, err := createChannelOAuthFlow(c, "codex", channel.Id, channelOAuthFlowPayload{Identity: identity, Verifier: "private-verifier"}, time.Now().Add(time.Minute))
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&channel).Update("type", constant.ChannelTypeOpenAI).Error)
	assert.False(t, consumeChannelOAuthFlow(c, "codex", channel.Id, token, identity, "new-credential"))
	var stored model.Channel
	require.NoError(t, model.DB.First(&stored, channel.Id).Error)
	assert.Equal(t, "original-key", stored.Key)
	require.NoError(t, model.DB.Model(&channel).Update("type", constant.ChannelTypeCodex).Error)
	require.True(t, consumeChannelOAuthFlow(c, "codex", channel.Id, token, identity, "new-credential"))
	require.NoError(t, model.DB.First(&stored, channel.Id).Error)
	assert.Equal(t, "new-credential", stored.Key)
	assert.False(t, consumeChannelOAuthFlow(c, "codex", channel.Id, token, identity, "replayed-credential"))
	require.NoError(t, model.DB.First(&stored, channel.Id).Error)
	assert.Equal(t, "new-credential", stored.Key)
}
