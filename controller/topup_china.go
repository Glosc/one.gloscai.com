package controller

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"html"
	"io"
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

const (
	alipayGatewayDefault = "https://openapi.alipay.com/gateway.do"
	wechatPayApiBase     = "https://api.mch.weixin.qq.com"
	wechatPayDescription = "Account recharge"
)

type officialPayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
}

type officialPayResponse struct {
	PayForm string `json:"pay_form,omitempty"`
	PayURL  string `json:"pay_url,omitempty"`
	QRCode  string `json:"qr_code,omitempty"`
	OrderID string `json:"order_id,omitempty"`
}

type officialPayStatusResponse struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
	Paid    bool   `json:"paid"`
}

type officialTradeQueryResult struct {
	Status    string
	PaidCents int
}

func RequestAlipayAmount(c *gin.Context) {
	requestOfficialAmount(c, isAlipayTopUpEnabled(), setting.AlipayMinTopUp, setting.AlipayUnitPrice)
}

func RequestWeChatPayAmount(c *gin.Context) {
	requestOfficialAmount(c, isWeChatPayTopUpEnabled(), setting.WeChatPayMinTopUp, setting.WeChatPayUnitPrice)
}

func requestOfficialAmount(c *gin.Context, enabled bool, minTopup int, unitPrice float64) {
	if !enabled {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "当前管理员未配置支付信息"})
		return
	}

	var req AmountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}

	minAmount := getMinTopupWithConfigured(minTopup)
	if req.Amount < minAmount {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", minAmount)})
		return
	}

	id := c.GetInt("id")
	if rejectInvalidTopUpQuota(c, id, req.Amount) {
		return
	}
	group, err := model.GetUserGroup(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	payMoney := getPayMoneyWithUnitPrice(req.Amount, group, unitPrice)
	if payMoney <= 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": strconv.FormatFloat(payMoney, 'f', 2, 64)})
}

func RequestAlipayPay(c *gin.Context) {
	if !isAlipayTopUpEnabled() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "当前管理员未配置支付信息"})
		return
	}
	requestOfficialPay(c, model.PaymentMethodAlipay, model.PaymentProviderAlipay, setting.AlipayMinTopUp, setting.AlipayUnitPrice, createAlipayPayment)
}

func RequestWeChatPay(c *gin.Context) {
	if !isWeChatPayTopUpEnabled() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "当前管理员未配置支付信息"})
		return
	}
	requestOfficialPay(c, model.PaymentMethodWeChatPay, model.PaymentProviderWeChatPay, setting.WeChatPayMinTopUp, setting.WeChatPayUnitPrice, createWeChatPayPayment)
}

func RequestAlipayPayStatus(c *gin.Context) {
	requestOfficialPayStatus(c, model.PaymentProviderAlipay, model.PaymentMethodAlipay, queryAlipayTrade)
}

func RequestWeChatPayStatus(c *gin.Context) {
	requestOfficialPayStatus(c, model.PaymentProviderWeChatPay, model.PaymentMethodWeChatPay, queryWeChatPayTrade)
}

func requestOfficialPayStatus(
	c *gin.Context,
	provider string,
	paymentMethod string,
	queryTrade func(string) (*officialTradeQueryResult, error),
) {
	tradeNo := strings.TrimSpace(c.Query("order_id"))
	if tradeNo == "" {
		common.ApiErrorMsg(c, "参数错误")
		return
	}

	userId := c.GetInt("id")
	topUp := model.GetTopUpByTradeNo(tradeNo)
	if topUp == nil || topUp.UserId != userId || topUp.PaymentProvider != provider {
		common.ApiErrorMsg(c, "订单不存在")
		return
	}

	if topUp.Status == common.TopUpStatusPending {
		result, err := queryTrade(tradeNo)
		if err != nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("官方支付 查询订单状态失败 provider=%s trade_no=%s user_id=%d error=%q", provider, tradeNo, userId, err.Error()))
		} else if result != nil {
			switch result.Status {
			case common.TopUpStatusSuccess:
				if err := completeOfficialTopUp(c, tradeNo, provider, paymentMethod, result.PaidCents); err != nil {
					logger.LogError(c.Request.Context(), fmt.Sprintf("官方支付 主动查单入账失败 provider=%s trade_no=%s user_id=%d error=%q", provider, tradeNo, userId, err.Error()))
					common.ApiErrorMsg(c, "支付已完成，但入账失败，请联系管理员")
					return
				}
			case common.TopUpStatusFailed, common.TopUpStatusExpired:
				if err := model.UpdatePendingTopUpStatus(tradeNo, provider, result.Status); err != nil &&
					!errors.Is(err, model.ErrTopUpStatusInvalid) &&
					!errors.Is(err, model.ErrTopUpNotFound) {
					logger.LogWarn(c.Request.Context(), fmt.Sprintf("官方支付 更新失败订单状态失败 provider=%s trade_no=%s status=%s error=%q", provider, tradeNo, result.Status, err.Error()))
				}
			}
			topUp = model.GetTopUpByTradeNo(tradeNo)
		}
	}

	status := common.TopUpStatusPending
	if topUp != nil {
		status = topUp.Status
	}
	common.ApiSuccess(c, officialPayStatusResponse{
		OrderID: tradeNo,
		Status:  status,
		Paid:    status == common.TopUpStatusSuccess,
	})
}

func requestOfficialPay(
	c *gin.Context,
	expectedMethod string,
	provider string,
	minTopup int,
	unitPrice float64,
	createPayment func(*gin.Context, string, string, float64) (*officialPayResponse, error),
) {
	var req officialPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	if req.PaymentMethod != expectedMethod {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "不支持的支付渠道"})
		return
	}

	minAmount := getMinTopupWithConfigured(minTopup)
	if req.Amount < minAmount {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", minAmount)})
		return
	}

	id := c.GetInt("id")
	if rejectInvalidTopUpQuota(c, id, req.Amount) {
		return
	}
	group, err := model.GetUserGroup(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}

	payMoney := getPayMoneyWithUnitPrice(req.Amount, group, unitPrice)
	if payMoney < 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	tradeNo := newOfficialTradeNo(provider, id)
	amount := normalizeTopUpAmountForStorage(req.Amount)
	topUp := &model.TopUp{
		UserId:          id,
		Amount:          amount,
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   expectedMethod,
		PaymentProvider: provider,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("官方支付 创建充值订单失败 provider=%s user_id=%d trade_no=%s amount=%d error=%q", provider, id, tradeNo, req.Amount, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	response, err := createPayment(c, tradeNo, fmt.Sprintf("TUC%d", req.Amount), payMoney)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("官方支付 拉起支付失败 provider=%s user_id=%d trade_no=%s amount=%d error=%q", provider, id, tradeNo, req.Amount, err.Error()))
		topUp.Status = common.TopUpStatusFailed
		_ = topUp.Update()
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	response.OrderID = tradeNo
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("官方支付 充值订单创建成功 provider=%s user_id=%d trade_no=%s amount=%d money=%.2f", provider, id, tradeNo, req.Amount, payMoney))
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": response})
}

func normalizeTopUpAmountForStorage(amount int64) int64 {
	if operation_setting.GetQuotaDisplayType() != operation_setting.QuotaDisplayTypeTokens {
		return amount
	}
	dAmount := decimal.NewFromInt(amount)
	dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
	return dAmount.Div(dQuotaPerUnit).IntPart()
}

func newOfficialTradeNo(provider string, userId int) string {
	source := fmt.Sprintf("%s-%d-%d-%s", provider, userId, time.Now().UnixNano(), common.GetRandomString(8))
	prefix := "CN"
	switch provider {
	case model.PaymentProviderAlipay:
		prefix = "ALI"
	case model.PaymentProviderWeChatPay:
		prefix = "WXP"
	}
	return strings.ToUpper(prefix + common.Sha1([]byte(source))[:24])
}

func callbackURL(customURL string, path string) string {
	if strings.TrimSpace(customURL) != "" {
		return strings.TrimSpace(customURL)
	}
	return strings.TrimRight(service.GetCallbackAddress(), "/") + path
}

func returnURL(customURL string) string {
	if strings.TrimSpace(customURL) != "" {
		return strings.TrimSpace(customURL)
	}
	return paymentReturnPath("/console/topup?show_history=true")
}

func createAlipayPayment(_ *gin.Context, tradeNo string, subject string, money float64) (*officialPayResponse, error) {
	product := setting.NormalizeAlipayProduct(setting.AlipayProduct)
	switch product {
	case setting.AlipayProductQRCode:
		payURL, err := buildAlipayPageURL("alipay.trade.page.pay", "FAST_INSTANT_TRADE_PAY", tradeNo, subject, money, map[string]interface{}{
			"qr_pay_mode":  "4",
			"qrcode_width": 200,
		})
		if err != nil {
			return nil, err
		}
		return &officialPayResponse{PayURL: payURL}, nil
	case setting.AlipayProductWap:
		form, err := buildAlipayPageForm("alipay.trade.wap.pay", "QUICK_WAP_WAY", tradeNo, subject, money, nil)
		if err != nil {
			return nil, err
		}
		return &officialPayResponse{PayForm: form}, nil
	default:
		form, err := buildAlipayPageForm("alipay.trade.page.pay", "FAST_INSTANT_TRADE_PAY", tradeNo, subject, money, nil)
		if err != nil {
			return nil, err
		}
		return &officialPayResponse{PayForm: form}, nil
	}
}

func alipayGateway() string {
	if strings.TrimSpace(setting.AlipayGateway) == "" {
		return alipayGatewayDefault
	}
	return strings.TrimSpace(setting.AlipayGateway)
}

func alipayBaseParams(method string) map[string]string {
	return map[string]string{
		"app_id":     strings.TrimSpace(setting.AlipayAppId),
		"method":     method,
		"format":     "JSON",
		"charset":    "utf-8",
		"sign_type":  "RSA2",
		"timestamp":  time.Now().Format("2006-01-02 15:04:05"),
		"version":    "1.0",
		"notify_url": callbackURL(setting.AlipayNotifyUrl, "/api/alipay/notify"),
	}
}

func buildAlipayPageForm(method string, productCode string, tradeNo string, subject string, money float64, extras map[string]interface{}) (string, error) {
	params, err := alipayPageParams(method, productCode, tradeNo, subject, money, extras)
	if err != nil {
		return "", err
	}
	return buildAutoSubmitForm(alipayGateway(), params), nil
}

func buildAlipayPageURL(method string, productCode string, tradeNo string, subject string, money float64, extras map[string]interface{}) (string, error) {
	params, err := alipayPageParams(method, productCode, tradeNo, subject, money, extras)
	if err != nil {
		return "", err
	}
	return buildGatewayURL(alipayGateway(), params), nil
}

func alipayPageParams(method string, productCode string, tradeNo string, subject string, money float64, extras map[string]interface{}) (map[string]string, error) {
	bizContent, err := alipayPageBizContent(productCode, tradeNo, subject, money, extras)
	if err != nil {
		return nil, err
	}

	params := alipayBaseParams(method)
	params["return_url"] = returnURL(setting.AlipayReturnUrl)
	params["biz_content"] = bizContent
	if err := signAlipayParams(params); err != nil {
		return nil, err
	}
	return params, nil
}

func alipayPageBizContent(productCode string, tradeNo string, subject string, money float64, extras map[string]interface{}) (string, error) {
	content := map[string]interface{}{
		"out_trade_no": tradeNo,
		"total_amount": formatYuanAmount(money),
		"subject":      subject,
		"product_code": productCode,
	}
	for key, value := range extras {
		if key != "" && value != nil {
			content[key] = value
		}
	}
	bizContent, err := common.Marshal(content)
	if err != nil {
		return "", err
	}
	return string(bizContent), nil
}

type alipayPrecreateResponse struct {
	Response struct {
		Code       string `json:"code"`
		Msg        string `json:"msg"`
		SubCode    string `json:"sub_code"`
		SubMsg     string `json:"sub_msg"`
		OutTradeNo string `json:"out_trade_no"`
		QRCode     string `json:"qr_code"`
	} `json:"alipay_trade_precreate_response"`
	Sign string `json:"sign"`
}

func requestAlipayPrecreate(tradeNo string, subject string, money float64) (string, error) {
	bizContent, err := common.Marshal(map[string]string{
		"out_trade_no": tradeNo,
		"total_amount": formatYuanAmount(money),
		"subject":      subject,
	})
	if err != nil {
		return "", err
	}

	params := alipayBaseParams("alipay.trade.precreate")
	params["biz_content"] = string(bizContent)
	if err := signAlipayParams(params); err != nil {
		return "", err
	}

	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	req, err := http.NewRequest(http.MethodPost, alipayGateway(), strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("alipay http status %d: %s", resp.StatusCode, string(body))
	}

	var parsed alipayPrecreateResponse
	if err := common.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if parsed.Response.Code != "10000" || parsed.Response.QRCode == "" {
		return "", fmt.Errorf("alipay precreate failed code=%s sub_code=%s msg=%s sub_msg=%s", parsed.Response.Code, parsed.Response.SubCode, parsed.Response.Msg, parsed.Response.SubMsg)
	}
	return parsed.Response.QRCode, nil
}

type alipayTradeQueryResponse struct {
	Response struct {
		Code        string `json:"code"`
		Msg         string `json:"msg"`
		SubCode     string `json:"sub_code"`
		SubMsg      string `json:"sub_msg"`
		OutTradeNo  string `json:"out_trade_no"`
		TradeNo     string `json:"trade_no"`
		TradeStatus string `json:"trade_status"`
		TotalAmount string `json:"total_amount"`
	} `json:"alipay_trade_query_response"`
	Sign string `json:"sign"`
}

func queryAlipayTrade(tradeNo string) (*officialTradeQueryResult, error) {
	bizContent, err := common.Marshal(map[string]string{"out_trade_no": tradeNo})
	if err != nil {
		return nil, err
	}

	params := alipayBaseParams("alipay.trade.query")
	delete(params, "notify_url")
	params["biz_content"] = string(bizContent)
	if err := signAlipayParams(params); err != nil {
		return nil, err
	}

	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	req, err := http.NewRequest(http.MethodPost, alipayGateway(), strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("alipay trade query http status %d: %s", resp.StatusCode, string(body))
	}

	var parsed alipayTradeQueryResponse
	if err := common.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if parsed.Response.Code != "10000" {
		if parsed.Response.SubCode == "ACQ.TRADE_NOT_EXIST" {
			return &officialTradeQueryResult{Status: common.TopUpStatusPending}, nil
		}
		return nil, fmt.Errorf("alipay trade query failed code=%s sub_code=%s msg=%s sub_msg=%s", parsed.Response.Code, parsed.Response.SubCode, parsed.Response.Msg, parsed.Response.SubMsg)
	}
	return alipayTradeStatusResult(parsed.Response.TradeStatus, parsed.Response.TotalAmount)
}

func alipayTradeStatusResult(tradeStatus string, totalAmount string) (*officialTradeQueryResult, error) {
	switch tradeStatus {
	case "TRADE_SUCCESS", "TRADE_FINISHED":
		paidCents, err := yuanStringToCents(totalAmount)
		if err != nil {
			return nil, err
		}
		return &officialTradeQueryResult{Status: common.TopUpStatusSuccess, PaidCents: paidCents}, nil
	case "TRADE_CLOSED":
		return &officialTradeQueryResult{Status: common.TopUpStatusFailed}, nil
	default:
		return &officialTradeQueryResult{Status: common.TopUpStatusPending}, nil
	}
}

func signAlipayParams(params map[string]string) error {
	privateKey, err := parseRSAPrivateKey(setting.AlipayPrivateKey)
	if err != nil {
		return err
	}
	hash := sha256.Sum256([]byte(alipayRequestSignContent(params)))
	sig, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return err
	}
	params["sign"] = base64.StdEncoding.EncodeToString(sig)
	return nil
}

func verifyAlipayParams(params map[string]string) bool {
	if strings.ToUpper(strings.TrimSpace(params["sign_type"])) != "RSA2" {
		return false
	}
	signature, err := base64.StdEncoding.DecodeString(params["sign"])
	if err != nil {
		return false
	}
	publicKey, err := parseRSAPublicKey(setting.AlipayPublicKey)
	if err != nil {
		return false
	}
	hash := sha256.Sum256([]byte(alipayNotifySignContent(params)))
	return rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, hash[:], signature) == nil
}

func alipayRequestSignContent(params map[string]string) string {
	return alipaySortedSignContent(params, false)
}

func alipayNotifySignContent(params map[string]string) string {
	return alipaySortedSignContent(params, true)
}

func alipaySortedSignContent(params map[string]string, excludeSignType bool) string {
	keys := make([]string, 0, len(params))
	for key, value := range params {
		if key == "sign" || (excludeSignType && key == "sign_type") || value == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+params[key])
	}
	return strings.Join(parts, "&")
}

func buildAutoSubmitForm(action string, params map[string]string) string {
	var builder strings.Builder
	builder.WriteString(`<!doctype html><html><head><meta charset="utf-8"></head><body>`)
	builder.WriteString(`<form id="pay-form" method="post" action="`)
	builder.WriteString(html.EscapeString(action))
	builder.WriteString(`">`)
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		builder.WriteString(`<input type="hidden" name="`)
		builder.WriteString(html.EscapeString(key))
		builder.WriteString(`" value="`)
		builder.WriteString(html.EscapeString(params[key]))
		builder.WriteString(`">`)
	}
	builder.WriteString(`</form><script>document.getElementById('pay-form').submit();</script></body></html>`)
	return builder.String()
}

func buildGatewayURL(action string, params map[string]string) string {
	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	separator := "?"
	if strings.Contains(action, "?") {
		separator = "&"
	}
	return action + separator + values.Encode()
}

func AlipayNotify(c *gin.Context) {
	if !isAlipayWebhookEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("支付宝 webhook 被拒绝 reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if err := c.Request.ParseForm(); err != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	params := make(map[string]string, len(c.Request.Form))
	for key := range c.Request.Form {
		params[key] = c.Request.Form.Get(key)
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("支付宝 webhook 收到请求 path=%q client_ip=%s params=%q", c.Request.RequestURI, c.ClientIP(), common.GetJsonString(params)))

	if params["app_id"] != strings.TrimSpace(setting.AlipayAppId) || !verifyAlipayParams(params) {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("支付宝 webhook 验签失败 trade_no=%s client_ip=%s", params["out_trade_no"], c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	tradeStatus := params["trade_status"]
	if tradeStatus != "TRADE_SUCCESS" && tradeStatus != "TRADE_FINISHED" {
		_, _ = c.Writer.Write([]byte("success"))
		return
	}

	paidCents, err := yuanStringToCents(params["total_amount"])
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("支付宝 webhook 金额解析失败 trade_no=%s amount=%q error=%q", params["out_trade_no"], params["total_amount"], err.Error()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	if err := completeOfficialTopUp(c, params["out_trade_no"], model.PaymentProviderAlipay, model.PaymentMethodAlipay, paidCents); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 webhook 入账失败 trade_no=%s client_ip=%s error=%q", params["out_trade_no"], c.ClientIP(), err.Error()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	_, _ = c.Writer.Write([]byte("success"))
}

func createWeChatPayPayment(c *gin.Context, tradeNo string, subject string, money float64) (*officialPayResponse, error) {
	tradeType := setting.NormalizeWeChatPayTradeType(setting.WeChatPayTradeType)
	path := "/v3/pay/transactions/native"
	if tradeType == setting.WeChatPayTradeTypeH5 {
		path = "/v3/pay/transactions/h5"
	}

	amountTotal := decimal.NewFromFloat(money).Mul(decimal.NewFromInt(100)).Round(0).IntPart()
	if amountTotal <= 0 {
		return nil, errors.New("invalid wechat pay amount")
	}

	body := map[string]interface{}{
		"appid":        strings.TrimSpace(setting.WeChatPayAppId),
		"mchid":        strings.TrimSpace(setting.WeChatPayMchId),
		"description":  truncateRunes(subjectOrDefault(subject), 127),
		"out_trade_no": tradeNo,
		"time_expire":  time.Now().Add(30 * time.Minute).Format(time.RFC3339),
		"notify_url":   callbackURL(setting.WeChatPayNotifyUrl, "/api/wechat-pay/notify"),
		"amount": map[string]interface{}{
			"total":    amountTotal,
			"currency": "CNY",
		},
	}

	if tradeType == setting.WeChatPayTradeTypeH5 {
		body["scene_info"] = map[string]interface{}{
			"payer_client_ip": c.ClientIP(),
			"h5_info": map[string]string{
				"type": "Wap",
			},
		}
	}

	bodyBytes, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	respBody, err := weChatPayRequest(http.MethodPost, path, bodyBytes)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		CodeURL string `json:"code_url"`
		H5URL   string `json:"h5_url"`
	}
	if err := common.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}
	if tradeType == setting.WeChatPayTradeTypeH5 {
		if parsed.H5URL == "" {
			return nil, errors.New("wechat pay h5_url is empty")
		}
		payURL := parsed.H5URL
		if redirectURL := returnURL(setting.WeChatPayReturnUrl); redirectURL != "" {
			sep := "&"
			if !strings.Contains(payURL, "?") {
				sep = "?"
			}
			payURL += sep + "redirect_url=" + url.QueryEscape(redirectURL)
		}
		return &officialPayResponse{PayURL: payURL}, nil
	}
	if parsed.CodeURL == "" {
		return nil, errors.New("wechat pay code_url is empty")
	}
	return &officialPayResponse{QRCode: parsed.CodeURL}, nil
}

func weChatPayRequest(method string, canonicalURL string, body []byte) ([]byte, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(setting.WeChatPayApiBase), "/")
	if baseURL == "" {
		baseURL = wechatPayApiBase
	}
	authorization, err := weChatPayAuthorization(method, canonicalURL, string(body))
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(method, baseURL+canonicalURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "new-api-wechat-pay")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("wechat pay http status %d: %s", resp.StatusCode, string(respBody))
	}
	return respBody, nil
}

func queryWeChatPayTrade(tradeNo string) (*officialTradeQueryResult, error) {
	mchID := strings.TrimSpace(setting.WeChatPayMchId)
	canonicalURL := "/v3/pay/transactions/out-trade-no/" + url.PathEscape(tradeNo) + "?mchid=" + url.QueryEscape(mchID)
	body, err := weChatPayRequest(http.MethodGet, canonicalURL, nil)
	if err != nil {
		if strings.Contains(err.Error(), "ORDERNOTEXIST") {
			return &officialTradeQueryResult{Status: common.TopUpStatusPending}, nil
		}
		return nil, err
	}

	var transaction weChatPayTransaction
	if err := common.Unmarshal(body, &transaction); err != nil {
		return nil, err
	}
	return weChatPayTradeStateResult(transaction.TradeState, transaction.Amount.Total)
}

func weChatPayTradeStateResult(tradeState string, paidCents int64) (*officialTradeQueryResult, error) {
	switch tradeState {
	case "SUCCESS":
		if paidCents <= 0 {
			return nil, errors.New("wechat pay amount is empty")
		}
		return &officialTradeQueryResult{Status: common.TopUpStatusSuccess, PaidCents: int(paidCents)}, nil
	case "CLOSED", "REVOKED", "PAYERROR", "REFUND":
		return &officialTradeQueryResult{Status: common.TopUpStatusFailed}, nil
	default:
		return &officialTradeQueryResult{Status: common.TopUpStatusPending}, nil
	}
}

func weChatPayAuthorization(method string, canonicalURL string, body string) (string, error) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := common.GetRandomString(32)
	message := method + "\n" + canonicalURL + "\n" + timestamp + "\n" + nonce + "\n" + body + "\n"
	privateKey, err := parseRSAPrivateKey(setting.WeChatPayMerchantPrivateKey)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(message))
	sig, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}
	signature := base64.StdEncoding.EncodeToString(sig)
	return fmt.Sprintf(`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",signature="%s",timestamp="%s",serial_no="%s"`, strings.TrimSpace(setting.WeChatPayMchId), nonce, signature, timestamp, strings.TrimSpace(setting.WeChatPayMerchantSerialNo)), nil
}

type weChatPayNotifyPayload struct {
	ID           string `json:"id"`
	CreateTime   string `json:"create_time"`
	EventType    string `json:"event_type"`
	ResourceType string `json:"resource_type"`
	Summary      string `json:"summary"`
	Resource     struct {
		OriginalType   string `json:"original_type"`
		Algorithm      string `json:"algorithm"`
		Ciphertext     string `json:"ciphertext"`
		AssociatedData string `json:"associated_data"`
		Nonce          string `json:"nonce"`
	} `json:"resource"`
}

type weChatPayTransaction struct {
	AppId         string `json:"appid"`
	MchId         string `json:"mchid"`
	OutTradeNo    string `json:"out_trade_no"`
	TransactionId string `json:"transaction_id"`
	TradeType     string `json:"trade_type"`
	TradeState    string `json:"trade_state"`
	Amount        struct {
		Total    int64  `json:"total"`
		Currency string `json:"currency"`
	} `json:"amount"`
}

func WeChatPayNotify(c *gin.Context) {
	if !isWeChatPayWebhookEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("微信支付 webhook 被拒绝 reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "FAIL", "message": "webhook disabled"})
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": "FAIL", "message": "invalid body"})
		return
	}

	if !verifyWeChatPaySignature(c, body) {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("微信支付 webhook 验签失败 path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "FAIL", "message": "signature verification failed"})
		return
	}

	var payload weChatPayNotifyPayload
	if err := common.Unmarshal(body, &payload); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": "FAIL", "message": "invalid payload"})
		return
	}
	if payload.EventType != "TRANSACTION.SUCCESS" {
		c.Status(http.StatusNoContent)
		return
	}

	plain, err := decryptWeChatPayResource(payload)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("微信支付 webhook 解密失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": "FAIL", "message": "decrypt failed"})
		return
	}
	var transaction weChatPayTransaction
	if err := common.Unmarshal(plain, &transaction); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": "FAIL", "message": "invalid transaction"})
		return
	}

	if transaction.AppId != strings.TrimSpace(setting.WeChatPayAppId) || transaction.MchId != strings.TrimSpace(setting.WeChatPayMchId) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": "FAIL", "message": "merchant mismatch"})
		return
	}
	if transaction.TradeState != "SUCCESS" {
		c.Status(http.StatusNoContent)
		return
	}

	if err := completeOfficialTopUp(c, transaction.OutTradeNo, model.PaymentProviderWeChatPay, model.PaymentMethodWeChatPay, int(transaction.Amount.Total)); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("微信支付 webhook 入账失败 trade_no=%s client_ip=%s error=%q", transaction.OutTradeNo, c.ClientIP(), err.Error()))
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"code": "FAIL", "message": "fulfill failed"})
		return
	}

	c.Status(http.StatusNoContent)
}

func verifyWeChatPaySignature(c *gin.Context, body []byte) bool {
	serial := c.GetHeader("Wechatpay-Serial")
	if configuredSerial := strings.TrimSpace(setting.WeChatPayPlatformSerialNo); configuredSerial != "" && serial != configuredSerial {
		return false
	}
	signature, err := base64.StdEncoding.DecodeString(c.GetHeader("Wechatpay-Signature"))
	if err != nil {
		return false
	}
	publicKey, err := parseRSAPublicKey(setting.WeChatPayPlatformPublicKey)
	if err != nil {
		return false
	}
	message := c.GetHeader("Wechatpay-Timestamp") + "\n" + c.GetHeader("Wechatpay-Nonce") + "\n" + string(body) + "\n"
	hash := sha256.Sum256([]byte(message))
	return rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, hash[:], signature) == nil
}

func decryptWeChatPayResource(payload weChatPayNotifyPayload) ([]byte, error) {
	if payload.Resource.Algorithm != "AEAD_AES_256_GCM" {
		return nil, errors.New("unsupported wechat pay resource algorithm")
	}
	key := []byte(strings.TrimSpace(setting.WeChatPayApiV3Key))
	if len(key) != 32 {
		return nil, errors.New("wechat pay api v3 key must be 32 bytes")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(payload.Resource.Ciphertext)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, []byte(payload.Resource.Nonce), ciphertext, []byte(payload.Resource.AssociatedData))
}

func completeOfficialTopUp(c *gin.Context, tradeNo string, provider string, paymentMethod string, paidCents int) error {
	if tradeNo == "" {
		return errors.New("trade_no is empty")
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	topUp := model.GetTopUpByTradeNo(tradeNo)
	if topUp == nil {
		return model.ErrTopUpNotFound
	}
	if topUp.PaymentProvider != provider {
		return model.ErrPaymentMethodMismatch
	}
	expectedCents := int(decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromInt(100)).Round(0).IntPart())
	if expectedCents != paidCents {
		return fmt.Errorf("payment amount mismatch expected=%d actual=%d", expectedCents, paidCents)
	}
	return model.RechargeDirectTopUp(tradeNo, provider, paymentMethod, c.ClientIP())
}

func formatYuanAmount(amount float64) string {
	return decimal.NewFromFloat(amount).Round(2).StringFixed(2)
}

func yuanStringToCents(value string) (int, error) {
	amount, err := decimal.NewFromString(strings.TrimSpace(value))
	if err != nil {
		return 0, err
	}
	return int(amount.Mul(decimal.NewFromInt(100)).Round(0).IntPart()), nil
}

func subjectOrDefault(subject string) string {
	if strings.TrimSpace(subject) == "" {
		return wechatPayDescription
	}
	return strings.TrimSpace(subject)
}

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

func parseRSAPrivateKey(raw string) (*rsa.PrivateKey, error) {
	block, err := decodePEMOrBase64(raw, "PRIVATE KEY")
	if err != nil {
		return nil, err
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not RSA")
	}
	return key, nil
}

func parseRSAPublicKey(raw string) (*rsa.PublicKey, error) {
	block, err := decodePEMOrBase64(raw, "PUBLIC KEY")
	if err != nil {
		return nil, err
	}
	if key, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		if cert, certErr := x509.ParseCertificate(block.Bytes); certErr == nil {
			if key, ok := cert.PublicKey.(*rsa.PublicKey); ok {
				return key, nil
			}
		}
		return nil, err
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("public key is not RSA")
	}
	return key, nil
}

func decodePEMOrBase64(raw string, defaultType string) (*pem.Block, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("empty key")
	}
	if block, _ := pem.Decode([]byte(trimmed)); block != nil {
		return block, nil
	}
	compact := strings.NewReplacer("\r", "", "\n", "", " ", "", "\t", "").Replace(trimmed)
	decoded, err := base64.StdEncoding.DecodeString(compact)
	if err != nil {
		return nil, err
	}
	return &pem.Block{Type: defaultType, Bytes: decoded}, nil
}
