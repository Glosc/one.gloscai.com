package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func WeChatAuth(c *gin.Context) {
	handleOAuthProvider(c, "wechat")
}

type wechatBindRequest struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

func WeChatBind(c *gin.Context) {
	var req wechatBindRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorT(c, "Invalid request")
		return
	}
	query := c.Request.URL.Query()
	query.Set("code", req.Code)
	query.Set("state", req.State)
	c.Request.URL.RawQuery = query.Encode()
	handleOAuthProvider(c, "wechat")
}
