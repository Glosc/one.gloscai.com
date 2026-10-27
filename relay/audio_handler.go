package relay

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func AudioHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)

	audioReq, ok := info.Request.(*dto.AudioRequest)
	if !ok {
		return types.NewError(errors.New("invalid request type"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	request, err := common.DeepCopy(audioReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to AudioRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	ioReader, err := adaptor.ConvertAudioRequest(c, info, *request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	resp, err := adaptor.DoRequest(c, info, ioReader)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	statusCodeMappingStr := c.GetString("status_code_mapping")

	var httpResp *http.Response
	if resp != nil {
		httpResp = resp.(*http.Response)
		if httpResp.StatusCode != http.StatusOK {
			newAPIError = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
			// reset status code 重置状态码
			service.ResetStatusCode(newAPIError, statusCodeMappingStr)
			return newAPIError
		}
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}
	usageDto, _ := usage.(*dto.Usage)

	// 按秒计费: 当模型被显式设置为 BillingTypePerSecond 且采用固定价格时,
	// 把固定价格视为"每秒单价", 通过音频时长(秒) 放大最终额度。
	// 对 STT: 输入音频时长来自上游 usage 的 PromptTokens / AudioTokens (按
	// ceil(duration)/60*1000 编码), 反向还原即可。对 TTS: 输出音频时长由
	// adapter 写入 CompletionTokenDetails.AudioTokens, 同样反向还原。
	if usageDto != nil &&
		info.PriceData.UsePrice &&
		info.PriceData.ModelPrice > 0 &&
		model.GetModelBillingType(info.OriginModelName) == model.BillingTypePerSecond {
		seconds := audioUsageSeconds(usageDto)
		if seconds > 0 {
			info.PriceData.ModelPrice = info.PriceData.ModelPrice * seconds
		}
	}

	if usageDto != nil && (usageDto.CompletionTokenDetails.AudioTokens > 0 || usageDto.PromptTokensDetails.AudioTokens > 0) {
		service.PostAudioConsumeQuota(c, info, usageDto, nil)
	} else {
		service.PostTextConsumeQuota(c, info, usageDto, nil)
	}

	return nil
}

// audioUsageSeconds 反推音频时长(秒)。音频 token 数按 ceil(duration)/60*1000
// 编码, 因此 seconds ≈ tokens * 60 / 1000。优先使用 details 中拆分出来的音频
// token 数; 当 details 为空时, 回退到 PromptTokens / CompletionTokens 整体
// (此时模型多半是纯音频输入/输出, 整体 token 数即音频 token 数)。
func audioUsageSeconds(usage *dto.Usage) float64 {
	if usage == nil {
		return 0
	}
	audioTokens := usage.PromptTokensDetails.AudioTokens + usage.CompletionTokenDetails.AudioTokens
	if audioTokens == 0 {
		// STT 路径上, 上游 usage 经常只回 PromptTokens / TotalTokens, 而
		// 我们的 token_counter 已经把音频时长编码为 token 数, 因此把整体
		// PromptTokens 视为音频 token 是安全的近似。
		audioTokens = usage.PromptTokens
	}
	if audioTokens <= 0 {
		return 0
	}
	return float64(audioTokens) * 60.0 / 1000.0
}
