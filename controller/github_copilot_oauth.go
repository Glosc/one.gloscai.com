package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func StartGitHubCopilotOAuth(c *gin.Context) {
	startGitHubCopilotOAuthWithChannelID(c, 0)
}

func StartGitHubCopilotOAuthForChannel(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, fmt.Errorf("invalid channel id: %w", err))
		return
	}
	startGitHubCopilotOAuthWithChannelID(c, channelID)
}

func startGitHubCopilotOAuthWithChannelID(c *gin.Context, channelID int) {
	setAuthNoStore(c)
	identity, ok := requireBrowserSession(c)
	if !ok {
		return
	}
	if _, _, err := service.ValidateLoginSession(identity); err != nil {
		writeAuthSessionError(c, err)
		return
	}
	channelProxy, ok := getGitHubCopilotOAuthChannelProxy(c, channelID)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()

	flow, err := service.CreateGitHubCopilotDeviceCode(ctx, channelProxy)
	if err != nil {
		common.SysError("failed to start github copilot device flow: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "启动授权失败，请重试"})
		return
	}

	_, err = createChannelOAuthFlow(c, "github_copilot", channelID, channelOAuthFlowPayload{Identity: identity, DeviceCode: flow.DeviceCode, Interval: flow.Interval}, time.Now().Add(time.Duration(flow.ExpiresIn)*time.Second))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"authorize_url":    flow.VerificationURL,
			"verification_url": flow.VerificationURL,
			"user_code":        flow.UserCode,
			"expires_in":       flow.ExpiresIn,
			"interval":         flow.Interval,
		},
	})
}

func CompleteGitHubCopilotOAuth(c *gin.Context) {
	completeGitHubCopilotOAuthWithChannelID(c, 0)
}

func CompleteGitHubCopilotOAuthForChannel(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, fmt.Errorf("invalid channel id: %w", err))
		return
	}
	completeGitHubCopilotOAuthWithChannelID(c, channelID)
}

func completeGitHubCopilotOAuthWithChannelID(c *gin.Context, channelID int) {
	setAuthNoStore(c)
	channelProxy, ok := getGitHubCopilotOAuthChannelProxy(c, channelID)
	if !ok {
		return
	}

	flowToken, authFlow, payload, ok := getChannelOAuthFlow(c, "github_copilot", channelID)
	if !ok {
		return
	}
	if payload.DeviceCode == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "oauth flow not started or session expired"})
		return
	}
	remaining := time.Until(authFlow.ExpiresAt)
	if remaining > 65*time.Second {
		remaining = 65 * time.Second
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), remaining)
	defer cancel()

	githubToken, err := service.WaitGitHubCopilotDeviceAccessToken(ctx, payload.DeviceCode, payload.Interval, channelProxy)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrGitHubCopilotDeviceAuthorizationPending):
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "authorization pending, please complete GitHub authorization first"})
		case errors.Is(err, service.ErrGitHubCopilotDeviceAccessDenied):
			if !consumeChannelOAuthFlow(c, "github_copilot", channelID, flowToken, payload.Identity, "") {
				return
			}
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "authorization denied"})
		case errors.Is(err, service.ErrGitHubCopilotDeviceExpired):
			if !consumeChannelOAuthFlow(c, "github_copilot", channelID, flowToken, payload.Identity, "") {
				return
			}
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "device code expired, please restart authorization"})
		default:
			common.SysError("failed to complete github copilot device flow: " + err.Error())
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "授权失败，请重试"})
		}
		return
	}

	encoded, err := common.Marshal(gin.H{"github_token": githubToken})
	if err != nil {
		common.ApiError(c, err)
		return
	}

	if !consumeChannelOAuthFlow(c, "github_copilot", channelID, flowToken, payload.Identity, string(encoded)) {
		return
	}

	if channelID > 0 {
		model.InitChannelCache()
		service.ResetProxyClientCache()
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "saved",
			"data": gin.H{
				"channel_id":   channelID,
				"channel_type": constant.ChannelTypeGitHubCopilot,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "generated",
		"data": gin.H{
			"key": string(encoded),
		},
	})
}

func getGitHubCopilotOAuthChannelProxy(c *gin.Context, channelID int) (string, bool) {
	if channelID <= 0 {
		return "", true
	}
	ch, err := model.GetChannelById(channelID, false)
	if err != nil {
		common.ApiError(c, err)
		return "", false
	}
	if ch == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "channel not found"})
		return "", false
	}
	if ch.Type != constant.ChannelTypeGitHubCopilot {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "channel type is not GitHub Copilot"})
		return "", false
	}
	return ch.GetSetting().Proxy, true
}
