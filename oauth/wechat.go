package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func init() {
	Register("wechat", &WeChatProvider{})
}

type WeChatProvider struct{}

type weChatTokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	OpenID       string `json:"openid"`
	Scope        string `json:"scope"`
	UnionID      string `json:"unionid"`
	ErrCode      int    `json:"errcode"`
	ErrMsg       string `json:"errmsg"`
}

type weChatUserInfoResponse struct {
	OpenID     string `json:"openid"`
	Nickname   string `json:"nickname"`
	HeadImgURL string `json:"headimgurl"`
	UnionID    string `json:"unionid"`
	ErrCode    int    `json:"errcode"`
	ErrMsg     string `json:"errmsg"`
}

func (p *WeChatProvider) GetName() string {
	return "WeChat"
}

func (p *WeChatProvider) IsEnabled() bool {
	return common.WeChatAuthEnabled && common.WeChatAppId != "" && common.WeChatAppSecret != ""
}

func (p *WeChatProvider) ExchangeToken(ctx context.Context, code string, c *gin.Context) (*OAuthToken, error) {
	if code == "" {
		return nil, NewOAuthError(common.NewMessage(msgInvalidCode))
	}

	endpoint := common.GetEnvOrDefaultString("WECHAT_OAUTH_TOKEN_ENDPOINT", "https://api.weixin.qq.com/sns/oauth2/access_token")
	requestURL, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	query := requestURL.Query()
	query.Set("appid", common.WeChatAppId)
	query.Set("secret", common.WeChatAppSecret)
	query.Set("code", code)
	query.Set("grant_type", "authorization_code")
	requestURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	client := http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-WeChat] ExchangeToken error: %s", err.Error()))
		return nil, NewOAuthErrorWithRaw(common.NewMessage(msgConnectFailed, map[string]any{"provider": "WeChat"}), err.Error())
	}
	defer res.Body.Close()

	logger.LogDebug(ctx, "[OAuth-WeChat] ExchangeToken response status: %d", res.StatusCode)

	var tokenResponse weChatTokenResponse
	if err := common.DecodeJson(res.Body, &tokenResponse); err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-WeChat] ExchangeToken decode error: %s", err.Error()))
		return nil, err
	}

	if tokenResponse.ErrCode != 0 {
		raw := fmt.Sprintf("wechat errcode=%d errmsg=%s", tokenResponse.ErrCode, tokenResponse.ErrMsg)
		logger.LogError(ctx, fmt.Sprintf("[OAuth-WeChat] ExchangeToken failed: %s", raw))
		return nil, NewOAuthErrorWithRaw(common.NewMessage(msgTokenFailed, map[string]any{"provider": "WeChat"}), raw)
	}
	if tokenResponse.AccessToken == "" || tokenResponse.OpenID == "" {
		logger.LogError(ctx, "[OAuth-WeChat] ExchangeToken failed: empty access token or openid")
		return nil, NewOAuthError(common.NewMessage(msgTokenFailed, map[string]any{"provider": "WeChat"}))
	}

	return &OAuthToken{
		AccessToken:  tokenResponse.AccessToken,
		RefreshToken: tokenResponse.RefreshToken,
		ExpiresIn:    tokenResponse.ExpiresIn,
		Scope:        tokenResponse.Scope,
		Extra: map[string]any{
			"openid":  tokenResponse.OpenID,
			"unionid": tokenResponse.UnionID,
		},
	}, nil
}

func (p *WeChatProvider) GetUserInfo(ctx context.Context, token *OAuthToken) (*OAuthUser, error) {
	openID, _ := token.Extra["openid"].(string)
	unionID, _ := token.Extra["unionid"].(string)
	userInfo := p.fetchUserInfo(ctx, token.AccessToken, openID)
	if userInfo != nil {
		if userInfo.OpenID != "" {
			openID = userInfo.OpenID
		}
		if userInfo.UnionID != "" {
			unionID = userInfo.UnionID
		}
	}

	providerUserID := unionID
	if providerUserID == "" {
		providerUserID = openID
	}
	if providerUserID == "" {
		logger.LogError(ctx, "[OAuth-WeChat] GetUserInfo failed: empty openid and unionid")
		return nil, NewOAuthError(common.NewMessage(msgUserInfoEmpty, map[string]any{"provider": "WeChat"}))
	}

	displayName := ""
	if userInfo != nil {
		displayName = userInfo.Nickname
	}
	logger.LogDebug(ctx, "[OAuth-WeChat] GetUserInfo success: openid=%s unionid=%s", openID, unionID)

	return &OAuthUser{
		ProviderUserID: providerUserID,
		DisplayName:    displayName,
		Extra: map[string]any{
			"openid":  openID,
			"unionid": unionID,
		},
	}, nil
}

func (p *WeChatProvider) fetchUserInfo(ctx context.Context, accessToken string, openID string) *weChatUserInfoResponse {
	if accessToken == "" || openID == "" {
		return nil
	}

	endpoint := common.GetEnvOrDefaultString("WECHAT_USERINFO_ENDPOINT", "https://api.weixin.qq.com/sns/userinfo")
	requestURL, err := url.Parse(endpoint)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("[OAuth-WeChat] GetUserInfo invalid endpoint: %s", err.Error()))
		return nil
	}
	query := requestURL.Query()
	query.Set("access_token", accessToken)
	query.Set("openid", openID)
	query.Set("lang", "zh_CN")
	requestURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("[OAuth-WeChat] GetUserInfo request error: %s", err.Error()))
		return nil
	}
	req.Header.Set("Accept", "application/json")

	client := http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("[OAuth-WeChat] GetUserInfo error: %s", err.Error()))
		return nil
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		logger.LogWarn(ctx, fmt.Sprintf("[OAuth-WeChat] GetUserInfo failed: status=%d", res.StatusCode))
		return nil
	}

	var userInfo weChatUserInfoResponse
	if err := common.DecodeJson(res.Body, &userInfo); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("[OAuth-WeChat] GetUserInfo decode error: %s", err.Error()))
		return nil
	}
	if userInfo.ErrCode != 0 {
		logger.LogWarn(ctx, fmt.Sprintf("[OAuth-WeChat] GetUserInfo failed: errcode=%d errmsg=%s", userInfo.ErrCode, userInfo.ErrMsg))
		return nil
	}

	return &userInfo
}

func (p *WeChatProvider) IsUserIDTaken(providerUserID string) bool {
	return model.IsWeChatIdAlreadyTaken(providerUserID)
}

func (p *WeChatProvider) FillUserByProviderID(user *model.User, providerUserID string) error {
	user.WeChatId = providerUserID
	return user.FillUserByWeChatId()
}

func (p *WeChatProvider) SetProviderUserID(user *model.User, providerUserID string) {
	user.WeChatId = providerUserID
}

func (p *WeChatProvider) GetProviderPrefix() string {
	return "wechat_"
}

// ProviderUserIDColumn returns the users-table column storing this provider's user ID.
func (p *WeChatProvider) ProviderUserIDColumn() string {
	return "wechat_id"
}
