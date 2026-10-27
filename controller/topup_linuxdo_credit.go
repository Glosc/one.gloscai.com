package controller

import (
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

const linuxDOCreditUpstreamType = "epay"
const linuxDOCreditMaxTopUp = 4000

func linuxDOCreditSign(params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for key, value := range params {
		if key == "sign" || key == "sign_type" || value == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+params[key])
	}
	digest := md5.Sum([]byte(strings.Join(parts, "&") + secret))
	return hex.EncodeToString(digest[:])
}

func linuxDOCreditPaymentURL() (string, error) {
	gateway, err := url.Parse(strings.TrimSpace(setting.LinuxDOCreditGateway))
	if err != nil || gateway.Scheme != "https" || gateway.Hostname() == "" || gateway.User != nil || gateway.RawQuery != "" || gateway.Fragment != "" {
		return "", fmt.Errorf("invalid LINUX DO Credit gateway")
	}
	gateway.Path = strings.TrimRight(gateway.Path, "/") + "/pay/submit.php"
	gateway.RawQuery = ""
	gateway.Fragment = ""
	return gateway.String(), nil
}

func linuxDOCreditPayMoney(amount int64, group string) float64 {
	return getPayMoneyWithUnitPrice(amount, group, setting.LinuxDOCreditUnitPrice)
}

func validateLinuxDOCreditTopUpAmount(amount int64) error {
	minimum := getMinTopupWithConfigured(setting.LinuxDOCreditMinTopUp)
	if amount < minimum {
		return fmt.Errorf("充值数量不能小于 %d", minimum)
	}
	maximum := getMinTopupWithConfigured(linuxDOCreditMaxTopUp)
	if amount > maximum {
		return fmt.Errorf("充值数量不能大于 %d", maximum)
	}
	return nil
}

func RequestLinuxDOCreditAmount(c *gin.Context) {
	var req AmountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if !isLinuxDOCreditTopUpEnabled() {
		common.ApiErrorMsg(c, "LINUX DO Credit 支付未启用")
		return
	}
	if err := validateLinuxDOCreditTopUpAmount(req.Amount); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if rejectInvalidTopUpQuota(c, c.GetInt("id"), req.Amount) {
		return
	}
	group, err := model.GetUserGroup(c.GetInt("id"), true)
	if err != nil {
		common.ApiErrorMsg(c, "获取用户分组失败")
		return
	}
	payMoney := linuxDOCreditPayMoney(req.Amount, group)
	if payMoney < 0.01 {
		common.ApiErrorMsg(c, "充值金额过低")
		return
	}
	common.ApiSuccess(c, strconv.FormatFloat(payMoney, 'f', 2, 64))
}

func RequestLinuxDOCreditPay(c *gin.Context) {
	var req EpayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PaymentMethod != model.PaymentMethodLinuxDOCredit {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if !isLinuxDOCreditTopUpEnabled() {
		common.ApiErrorMsg(c, "LINUX DO Credit 支付未启用")
		return
	}
	if err := validateLinuxDOCreditTopUpAmount(req.Amount); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if rejectInvalidTopUpQuota(c, c.GetInt("id"), req.Amount) {
		return
	}
	group, err := model.GetUserGroup(c.GetInt("id"), true)
	if err != nil {
		common.ApiErrorMsg(c, "获取用户分组失败")
		return
	}
	payMoney := linuxDOCreditPayMoney(req.Amount, group)
	if payMoney < 0.01 {
		common.ApiErrorMsg(c, "充值金额过低")
		return
	}
	paymentURL, err := linuxDOCreditPaymentURL()
	if err != nil {
		common.ApiErrorMsg(c, "LINUX DO Credit 网关地址无效")
		return
	}

	userID := c.GetInt("id")
	tradeNo := fmt.Sprintf("LDC%dNO%s%d", userID, common.GetRandomString(6), time.Now().Unix())
	params := map[string]string{
		"pid":          strings.TrimSpace(setting.LinuxDOCreditClientID),
		"type":         linuxDOCreditUpstreamType,
		"out_trade_no": tradeNo,
		"name":         fmt.Sprintf("TUC%d", req.Amount),
		"money":        strconv.FormatFloat(payMoney, 'f', 2, 64),
		"notify_url":   strings.TrimRight(service.GetCallbackAddress(), "/") + "/api/user/linuxdo-credit/notify",
		"return_url":   paymentReturnPath("/console/topup?show_history=true"),
		"device":       "pc",
		"sign_type":    "MD5",
	}
	params["sign"] = linuxDOCreditSign(params, setting.LinuxDOCreditSecret)

	amount := req.Amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		amount = decimal.NewFromInt(amount).Div(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart()
	}
	topUp := &model.TopUp{
		UserId:          userID,
		Amount:          amount,
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   model.PaymentMethodLinuxDOCredit,
		PaymentProvider: model.PaymentProviderLinuxDOCredit,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("LINUX DO Credit 创建充值订单失败 user_id=%d trade_no=%s error=%q", userID, tradeNo, err.Error()))
		common.ApiErrorMsg(c, "创建订单失败")
		return
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("LINUX DO Credit 充值订单创建成功 user_id=%d trade_no=%s amount=%d ldc=%.2f", userID, tradeNo, req.Amount, payMoney))
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "success", "data": params, "url": paymentURL})
}

func LinuxDOCreditNotify(c *gin.Context) {
	if !isLinuxDOCreditWebhookEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("LINUX DO Credit webhook 被拒绝 path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		c.String(http.StatusOK, "fail")
		return
	}
	if err := c.Request.ParseForm(); err != nil {
		c.String(http.StatusOK, "fail")
		return
	}
	params := make(map[string]string, len(c.Request.Form))
	for key, values := range c.Request.Form {
		if len(values) != 1 {
			c.String(http.StatusOK, "fail")
			return
		}
		params[key] = values[0]
	}
	expectedSign := linuxDOCreditSign(params, setting.LinuxDOCreditSecret)
	receivedSign := strings.ToLower(strings.TrimSpace(params["sign"]))
	if len(receivedSign) != len(expectedSign) || subtle.ConstantTimeCompare([]byte(receivedSign), []byte(expectedSign)) != 1 {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("LINUX DO Credit webhook 验签失败 path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		c.String(http.StatusOK, "fail")
		return
	}
	if params["pid"] != strings.TrimSpace(setting.LinuxDOCreditClientID) ||
		params["type"] != linuxDOCreditUpstreamType ||
		params["trade_status"] != "TRADE_SUCCESS" {
		c.String(http.StatusOK, "fail")
		return
	}

	tradeNo := params["out_trade_no"]
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)
	topUp := model.GetTopUpByTradeNo(tradeNo)
	if topUp == nil || topUp.PaymentProvider != model.PaymentProviderLinuxDOCredit || topUp.PaymentMethod != model.PaymentMethodLinuxDOCredit {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("LINUX DO Credit 回调订单不匹配 trade_no=%s client_ip=%s", tradeNo, c.ClientIP()))
		c.String(http.StatusOK, "fail")
		return
	}
	callbackMoney, err := decimal.NewFromString(params["money"])
	if err != nil || !callbackMoney.Equal(decimal.NewFromFloat(topUp.Money).Round(2)) {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("LINUX DO Credit 回调金额不匹配 trade_no=%s expected=%.2f actual=%q", tradeNo, topUp.Money, params["money"]))
		c.String(http.StatusOK, "fail")
		return
	}
	if err := model.RechargeLinuxDOCreditTopUp(tradeNo, c.ClientIP()); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("LINUX DO Credit 充值处理失败 trade_no=%s error=%q", tradeNo, err.Error()))
		c.String(http.StatusOK, "fail")
		return
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("LINUX DO Credit 充值成功 trade_no=%s ldc=%.2f", tradeNo, topUp.Money))
	c.String(http.StatusOK, "success")
}
