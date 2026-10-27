package controller

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func Playground(c *gin.Context) {
	var newAPIError *types.NewAPIError

	defer func() {
		if newAPIError != nil {
			c.JSON(newAPIError.StatusCode, gin.H{
				"error": newAPIError.ToOpenAIError(common.UpstreamAddresses(c)...),
			})
		}
	}()

	useAccessToken := c.GetBool("use_access_token")
	if useAccessToken {
		newAPIError = types.NewError(errors.New(i18n.T(c, i18n.MsgRelayAccessTokenUnsupported)), types.ErrorCodeAccessDenied, types.ErrOptionWithSkipRetry())
		return
	}

	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatOpenAI, nil, nil)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
		return
	}

	userId := c.GetInt("id")

	// Write user context to ensure acceptUnsetRatio is available
	userCache, err := model.GetUserCache(userId)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		return
	}
	userCache.WriteContext(c)

	tempToken := &model.Token{
		UserId: userId,
		Name:   fmt.Sprintf("playground-%s", relayInfo.UsingGroup),
		Group:  relayInfo.UsingGroup,
	}
	_ = middleware.SetupContextForToken(c, tempToken)

	Relay(c, types.RelayFormatOpenAI)
}

// setupPlaygroundToken bridges dashboard session auth to the relay layer by
// constructing a temporary in-memory token bound to the current user/group.
// Returns a NewAPIError when the session cannot be used (e.g. access token).
func setupPlaygroundToken(c *gin.Context) *types.NewAPIError {
	if c.GetBool("use_access_token") {
		return types.NewErrorWithStatusCode(errors.New(i18n.T(c, i18n.MsgRelayAccessTokenUnsupported)), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}

	userId := c.GetInt("id")
	userCache, err := model.GetUserCache(userId)
	if err != nil {
		return types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	userCache.WriteContext(c)
	usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if usingGroup == "" {
		usingGroup = userCache.Group
		common.SetContextKey(c, constant.ContextKeyUsingGroup, usingGroup)
	}
	tempToken := &model.Token{
		UserId: userId,
		Name:   fmt.Sprintf("playground-%s", usingGroup),
		Group:  usingGroup,
	}
	_ = middleware.SetupContextForToken(c, tempToken)
	return nil
}

// PreparePlaygroundImage initializes session billing and the requested group
// before ordinary and plugin-backed image channels compete for the request.
func PreparePlaygroundImage(c *gin.Context) {
	if apiErr := setupPlaygroundToken(c); apiErr != nil {
		c.AbortWithStatusJSON(apiErr.StatusCode, gin.H{"error": apiErr.ToOpenAIError()})
		return
	}
	var request middleware.ModelRequest
	if err := common.UnmarshalBodyReusable(c, &request); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Invalid request body"}})
		return
	}
	userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	if request.Group != "" {
		if request.Group != userGroup && !service.GroupInUserUsableGroups(userGroup, request.Group) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": gin.H{"message": i18n.T(c, i18n.MsgDistributorGroupAccessDenied)}})
			return
		}
		common.SetContextKey(c, constant.ContextKeyUsingGroup, request.Group)
		common.SetContextKey(c, constant.ContextKeyTokenGroup, request.Group)
	}
	c.Next()
}

// PlaygroundImage shares the host image protocol bridge with the API while
// keeping the dashboard session's wallet and group context.
func PlaygroundImage(c *gin.Context) {
	RelayTaskPluginEndpoint(c, func(c *gin.Context) { Relay(c, types.RelayFormatOpenAIImage) })
}

// PlaygroundVideo proxies dashboard video-generation submissions through the
// relay task system using the current user's session.
func PlaygroundVideo(c *gin.Context) {
	if apiErr := setupPlaygroundToken(c); apiErr != nil {
		c.JSON(apiErr.StatusCode, gin.H{"error": apiErr.ToOpenAIError()})
		return
	}
	RelayTask(c)
}

// PlaygroundVideoFetch proxies dashboard video-task status polling through the
// relay task system using the current user's session.
func PlaygroundVideoFetch(c *gin.Context) {
	if apiErr := setupPlaygroundToken(c); apiErr != nil {
		c.JSON(apiErr.StatusCode, gin.H{"error": apiErr.ToOpenAIError()})
		return
	}
	RelayTaskFetch(c)
}

// PlaygroundAudioSpeech proxies dashboard text-to-speech (TTS) requests through
// the relay using the current user's session (no API key required).
func PlaygroundAudioSpeech(c *gin.Context) {
	if apiErr := setupPlaygroundToken(c); apiErr != nil {
		c.JSON(apiErr.StatusCode, gin.H{"error": apiErr.ToOpenAIError()})
		return
	}
	Relay(c, types.RelayFormatOpenAIAudio)
}

// PlaygroundAudioTranscription proxies dashboard speech-to-text (STT)
// transcription requests through the relay using the current user's session
// (no API key required).
func PlaygroundAudioTranscription(c *gin.Context) {
	if apiErr := setupPlaygroundToken(c); apiErr != nil {
		c.JSON(apiErr.StatusCode, gin.H{"error": apiErr.ToOpenAIError()})
		return
	}
	Relay(c, types.RelayFormatOpenAIAudio)
}
