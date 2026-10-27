package controller

import (
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Channel authorization keeps provider secrets on the server and binds the
// one-time browser token to the current login session and channel.
type channelOAuthFlowPayload struct {
	Identity   service.AuthIdentity `json:"identity"`
	Verifier   string               `json:"verifier,omitempty"`
	DeviceCode string               `json:"device_code,omitempty"`
	Interval   int                  `json:"interval,omitempty"`
}

func channelOAuthFlowProvider(provider string, channelID int) string {
	return fmt.Sprintf("channel:%s:%d", provider, channelID)
}

func writeChannelOAuthCookie(c *gin.Context, provider string, channelID int, token string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     fmt.Sprintf("channel_oauth_%s_%d", provider, channelID),
		Value:    token,
		Path:     "/api/channel",
		MaxAge:   maxAge,
		Secure:   common.SessionCookieSecure,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func createChannelOAuthFlow(c *gin.Context, provider string, channelID int, payload channelOAuthFlowPayload, expiresAt time.Time) (string, error) {
	if _, _, err := service.ValidateLoginSession(payload.Identity); err != nil {
		return "", err
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		return "", err
	}
	token, _, err := model.CreateAuthFlow(model.AuthFlowCreate{
		Purpose: "channel_oauth", Provider: channelOAuthFlowProvider(provider, channelID),
		Intent: model.AuthFlowIntentBind, UserId: payload.Identity.UserID, SessionId: payload.Identity.SessionID,
		Payload: string(encoded), ExpiresAt: expiresAt,
	})
	if err != nil {
		return "", err
	}
	writeChannelOAuthCookie(c, provider, channelID, token, max(1, int(time.Until(expiresAt).Seconds())))
	return token, nil
}

func getChannelOAuthFlow(c *gin.Context, provider string, channelID int) (string, *model.AuthFlow, channelOAuthFlowPayload, bool) {
	var payload channelOAuthFlowPayload
	identity, ok := requireBrowserSession(c)
	if !ok {
		return "", nil, payload, false
	}
	if _, _, err := service.ValidateLoginSession(identity); err != nil {
		writeAuthSessionError(c, err)
		return "", nil, payload, false
	}
	token, _ := c.Cookie(fmt.Sprintf("channel_oauth_%s_%d", provider, channelID))
	flow, err := model.GetAuthFlow(token, model.AuthFlowMatch{
		Purpose: "channel_oauth", Provider: channelOAuthFlowProvider(provider, channelID),
		Intent: model.AuthFlowIntentBind, UserId: identity.UserID, SessionId: identity.SessionID,
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "oauth flow not started or session expired"})
		return "", nil, payload, false
	}
	if err := common.UnmarshalJsonStr(flow.Payload, &payload); err != nil || payload.Identity != identity {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "oauth flow not started or session expired"})
		return "", nil, payload, false
	}
	return token, flow, payload, true
}

func consumeChannelOAuthFlow(c *gin.Context, provider string, channelID int, token string, identity service.AuthIdentity, credential string) bool {
	_, err := model.ConsumeAuthFlowWithAction(token, model.AuthFlowMatch{
		Purpose: "channel_oauth", Provider: channelOAuthFlowProvider(provider, channelID),
		Intent: model.AuthFlowIntentBind, UserId: identity.UserID, SessionId: identity.SessionID,
	}, func(tx *gorm.DB, _ *model.AuthFlow) error {
		if err := model.ValidateAuthSessionWithTx(tx, identity); err != nil {
			return err
		}
		if channelID <= 0 || credential == "" {
			return nil
		}
		channelType := constant.ChannelTypeCodex
		if provider == "github_copilot" {
			channelType = constant.ChannelTypeGitHubCopilot
		}
		result := tx.Model(&model.Channel{}).Where("id = ? AND type = ?", channelID, channelType).Update("key", credential)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return model.ErrAuthFlowInvalid
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "oauth flow not started or session expired"})
		return false
	}
	writeChannelOAuthCookie(c, provider, channelID, "", -1)
	return true
}
