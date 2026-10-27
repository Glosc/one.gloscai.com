package ali

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// ============================
// Request / Response structures
// ============================

// AliVideoRequest 阿里通义万相视频生成请求
type AliVideoRequest struct {
	Model      string              `json:"model"`
	Input      AliVideoInput       `json:"input"`
	Parameters *AliVideoParameters `json:"parameters,omitempty"`
}

// AliVideoMedia describes Wan2.7 image-to-video media inputs.
type AliVideoMedia struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// AliVideoInput 视频输入参数
type AliVideoInput struct {
	Prompt         string         `json:"prompt,omitempty"`          // 文本提示词
	ImgURL         string         `json:"img_url,omitempty"`         // 首帧图像URL或Base64（图生视频）
	FirstFrameURL  string         `json:"first_frame_url,omitempty"` // 首帧图片URL（首尾帧生视频）
	LastFrameURL   string         `json:"last_frame_url,omitempty"`  // 尾帧图片URL（首尾帧生视频）
	AudioURL       string         `json:"audio_url,omitempty"`       // 音频URL（wan2.5支持）
	NegativePrompt string         `json:"negative_prompt,omitempty"` // 反向提示词
	Template       string         `json:"template,omitempty"`        // 视频特效模板
	Media          []AliMediaItem `json:"media,omitempty"`           // 多模态素材（wan2.7图生视频）
}

// AliMediaItem is kept as a compatibility alias for existing metadata handling.
type AliMediaItem = AliVideoMedia

// AliVideoParameters 视频参数
type AliVideoParameters struct {
	Resolution   string `json:"resolution,omitempty"`    // 分辨率: 480P/720P/1080P（图生视频、首尾帧生视频）
	Ratio        string `json:"ratio,omitempty"`         // 宽高比: 16:9/9:16等（wan2.7文生视频）
	Size         string `json:"size,omitempty"`          // 尺寸: 如 "832*480"（文生视频）
	Duration     *int   `json:"duration,omitempty"`      // 时长: 3-10秒
	ShotType     string `json:"shot_type,omitempty"`     // 多镜头: multi（wan2.6及早期模型）
	PromptExtend *bool  `json:"prompt_extend,omitempty"` // 是否开启prompt智能改写
	Watermark    *bool  `json:"watermark,omitempty"`     // 是否添加水印
	Audio        *bool  `json:"audio,omitempty"`         // 是否添加音频（wan2.5）
	Seed         *int   `json:"seed,omitempty"`          // 随机数种子
}

// AliVideoResponse 阿里通义万相响应
type AliVideoResponse struct {
	Output    AliVideoOutput `json:"output"`
	RequestID string         `json:"request_id"`
	Code      string         `json:"code,omitempty"`
	Message   string         `json:"message,omitempty"`
	Usage     *AliUsage      `json:"usage,omitempty"`
}

// AliVideoOutput 输出信息
type AliVideoOutput struct {
	TaskID        string `json:"task_id"`
	TaskStatus    string `json:"task_status"`
	SubmitTime    string `json:"submit_time,omitempty"`
	ScheduledTime string `json:"scheduled_time,omitempty"`
	EndTime       string `json:"end_time,omitempty"`
	OrigPrompt    string `json:"orig_prompt,omitempty"`
	ActualPrompt  string `json:"actual_prompt,omitempty"`
	VideoURL      string `json:"video_url,omitempty"`
	Code          string `json:"code,omitempty"`
	Message       string `json:"message,omitempty"`
}

// AliUsage 使用统计
type AliUsage struct {
	Duration   dto.IntValue `json:"duration,omitempty"`
	VideoCount dto.IntValue `json:"video_count,omitempty"`
	SR         dto.IntValue `json:"SR,omitempty"`
}

type AliMetadata struct {
	// Input 相关
	AudioURL       string         `json:"audio_url,omitempty"`       // 音频URL
	ImgURL         string         `json:"img_url,omitempty"`         // 图片URL（图生视频）
	FirstFrameURL  string         `json:"first_frame_url,omitempty"` // 首帧图片URL（首尾帧生视频）
	LastFrameURL   string         `json:"last_frame_url,omitempty"`  // 尾帧图片URL（首尾帧生视频）
	NegativePrompt string         `json:"negative_prompt,omitempty"` // 反向提示词
	Template       string         `json:"template,omitempty"`        // 视频特效模板
	Media          []AliMediaItem `json:"media,omitempty"`           // 多模态素材

	// Parameters 相关
	Resolution   *string `json:"resolution,omitempty"`    // 分辨率: 480P/720P/1080P
	Ratio        *string `json:"ratio,omitempty"`         // 宽高比
	Size         *string `json:"size,omitempty"`          // 尺寸: 如 "832*480"
	Duration     *int    `json:"duration,omitempty"`      // 时长
	ShotType     *string `json:"shot_type,omitempty"`     // 多镜头
	PromptExtend *bool   `json:"prompt_extend,omitempty"` // 是否开启prompt智能改写
	Watermark    *bool   `json:"watermark,omitempty"`     // 是否添加水印
	Audio        *bool   `json:"audio,omitempty"`         // 是否添加音频
	Seed         *int    `json:"seed,omitempty"`          // 随机数种子
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *taskdto.TaskError) {
	// ValidateMultipartDirect 负责解析并将原始 TaskSubmitReq 存入 context
	return relaycommon.ValidateMultipartDirect(c, info)
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	modelName := info.UpstreamModelName
	if modelName == "" {
		modelName = info.OriginModelName
	}
	if strings.Contains(modelName, "kf2v") {
		return fmt.Sprintf("%s/api/v1/services/aigc/image2video/video-synthesis", a.baseURL), nil
	}
	return fmt.Sprintf("%s/api/v1/services/aigc/video-generation/video-synthesis", a.baseURL), nil
}

// BuildRequestHeader sets required headers for Ali API
func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-DashScope-Async", "enable") // 阿里异步任务必须设置
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	taskReq, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, errors.Wrap(err, "get_task_request_failed")
	}

	aliReq, err := a.convertToAliRequest(info, taskReq)
	if err != nil {
		return nil, errors.Wrap(err, "convert_to_ali_request_failed")
	}
	logger.LogJson(c, "ali video request body", aliReq)

	bodyBytes, err := common.Marshal(aliReq)
	if err != nil {
		return nil, errors.Wrap(err, "marshal_ali_request_failed")
	}
	return bytes.NewReader(bodyBytes), nil
}

var (
	size480p = []string{
		"832*480",
		"480*832",
		"624*624",
	}
	size720p = []string{
		"1280*720",
		"720*1280",
		"960*960",
		"1088*832",
		"832*1088",
	}
	size1080p = []string{
		"1920*1080",
		"1080*1920",
		"1440*1440",
		"1632*1248",
		"1248*1632",
	}
)

func sizeToResolution(size string) (string, error) {
	if lo.Contains(size480p, size) {
		return "480P", nil
	} else if lo.Contains(size720p, size) {
		return "720P", nil
	} else if lo.Contains(size1080p, size) {
		return "1080P", nil
	}
	return "", fmt.Errorf("invalid size: %s", size)
}

func intValue(ptr *int, fallback int) int {
	if ptr == nil {
		return fallback
	}
	return *ptr
}

func isWan27Model(modelName string) bool {
	return strings.Contains(strings.ToLower(modelName), "wan2.7")
}

func isWan27I2VModel(modelName string) bool {
	modelName = strings.ToLower(modelName)
	return strings.Contains(modelName, "wan2.7") &&
		(strings.Contains(modelName, "i2v") || strings.Contains(modelName, "image2video"))
}

func appendModelCandidate(candidates []string, modelName string) []string {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return candidates
	}
	if lo.Contains(candidates, modelName) {
		return candidates
	}
	return append(candidates, modelName)
}

func aliModelCandidates(info *relaycommon.RelayInfo, aliReq *AliVideoRequest, req relaycommon.TaskSubmitReq) []string {
	candidates := make([]string, 0, 5)
	if aliReq != nil {
		candidates = appendModelCandidate(candidates, aliReq.Model)
	}
	candidates = appendModelCandidate(candidates, req.Model)
	if info != nil {
		candidates = appendModelCandidate(candidates, info.OriginModelName)
		if info.ChannelMeta != nil {
			candidates = appendModelCandidate(candidates, info.ChannelMeta.UpstreamModelName)
		}
	}
	return candidates
}

func anyWan27Model(candidates []string) bool {
	for _, candidate := range candidates {
		if isWan27Model(candidate) {
			return true
		}
	}
	return false
}

func anyWan27I2VModel(candidates []string) bool {
	for _, candidate := range candidates {
		if isWan27I2VModel(candidate) {
			return true
		}
	}
	return false
}

func normalizeWan27Resolution(resolution string) string {
	resolution = strings.ToUpper(strings.TrimSpace(resolution))
	if resolution == "" {
		return "720P"
	}
	if !strings.HasSuffix(resolution, "P") {
		resolution += "P"
	}
	if resolution != "720P" && resolution != "1080P" {
		return "720P"
	}
	return resolution
}

func stringFromMapValue(data map[string]any, keys ...string) string {
	current := any(data)
	for _, key := range keys {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = m[key]
		if !ok {
			return ""
		}
	}
	if value, ok := current.(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func firstMetadataMediaURL(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}

	for _, keys := range [][]string{
		{"input", "img_url"},
		{"input", "first_frame_url"},
		{"input", "image"},
		{"input", "input_reference"},
		{"img_url"},
		{"first_frame_url"},
		{"image"},
		{"input_reference"},
	} {
		if url := stringFromMapValue(metadata, keys...); url != "" {
			return url
		}
	}

	return ""
}

func firstAliMediaURL(aliReq *AliVideoRequest, req relaycommon.TaskSubmitReq) string {
	for _, url := range []string{
		aliReq.Input.ImgURL,
		aliReq.Input.FirstFrameURL,
		req.InputReference,
		req.Image,
		firstMetadataMediaURL(req.Metadata),
	} {
		if strings.TrimSpace(url) != "" {
			return strings.TrimSpace(url)
		}
	}
	if len(req.Images) > 0 {
		return strings.TrimSpace(req.Images[0])
	}
	return ""
}

func normalizeAliVideoRequestForModel(info *relaycommon.RelayInfo, aliReq *AliVideoRequest, req relaycommon.TaskSubmitReq) error {
	if aliReq.Parameters == nil {
		aliReq.Parameters = &AliVideoParameters{}
	}

	candidates := aliModelCandidates(info, aliReq, req)
	if anyWan27Model(candidates) {
		aliReq.Parameters.Resolution = normalizeWan27Resolution(aliReq.Parameters.Resolution)
		aliReq.Parameters.Size = ""
	}

	if !anyWan27I2VModel(candidates) {
		return nil
	}

	if len(aliReq.Input.Media) == 0 {
		if url := firstAliMediaURL(aliReq, req); url != "" {
			aliReq.Input.Media = []AliMediaItem{
				{Type: "first_frame", URL: url},
			}
		}
	}
	if len(aliReq.Input.Media) == 0 {
		return errors.New("wan2.7-i2v requires input.media with first_frame or first_clip")
	}

	// wan2.7-i2v uses input.media. Clear legacy image fields to avoid
	// sending an incompatible mixed payload to DashScope.
	aliReq.Input.ImgURL = ""
	aliReq.Input.FirstFrameURL = ""
	aliReq.Input.LastFrameURL = ""
	return nil
}

func ProcessAliOtherRatios(aliReq *AliVideoRequest) (map[string]float64, error) {
	otherRatios := make(map[string]float64)
	aliRatios := map[string]map[string]float64{
		"wan2.6-i2v": {
			"720P":  1,
			"1080P": 1 / 0.6,
		},
		"wan2.5-t2v-preview": {
			"480P":  1,
			"720P":  2,
			"1080P": 1 / 0.3,
		},
		"wan2.2-t2v-plus": {
			"480P":  1,
			"1080P": 0.7 / 0.14,
		},
		"wan2.5-i2v-preview": {
			"480P":  1,
			"720P":  2,
			"1080P": 1 / 0.3,
		},
		"wan2.2-i2v-plus": {
			"480P":  1,
			"1080P": 0.7 / 0.14,
		},
		"wan2.2-kf2v-flash": {
			"480P":  1,
			"720P":  2,
			"1080P": 4.8,
		},
		"wan2.2-i2v-flash": {
			"480P": 1,
			"720P": 2,
		},
		"wan2.2-s2v": {
			"480P": 1,
			"720P": 0.9 / 0.5,
		},
	}
	var resolution string

	// size match
	if aliReq.Parameters.Size != "" {
		toResolution, err := sizeToResolution(aliReq.Parameters.Size)
		if err != nil {
			return nil, err
		}
		resolution = toResolution
	} else {
		resolution = strings.ToUpper(aliReq.Parameters.Resolution)
		if !strings.HasSuffix(resolution, "P") {
			resolution = resolution + "P"
		}
	}
	if otherRatio, ok := aliRatios[aliReq.Model]; ok {
		if ratio, ok := otherRatio[resolution]; ok {
			otherRatios[fmt.Sprintf("resolution-%s", resolution)] = ratio
		}
	}
	return otherRatios, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstTaskImage(req relaycommon.TaskSubmitReq) string {
	if image := strings.TrimSpace(req.Image); image != "" {
		return image
	}
	for _, image := range req.Images {
		if trimmed := strings.TrimSpace(image); trimmed != "" {
			return trimmed
		}
	}
	if inputReference := strings.TrimSpace(req.InputReference); inputReference != "" {
		return inputReference
	}
	return ""
}

func secondTaskImage(req relaycommon.TaskSubmitReq) string {
	nonEmptyImages := 0
	for _, image := range req.Images {
		trimmed := strings.TrimSpace(image)
		if trimmed == "" {
			continue
		}
		nonEmptyImages++
		if nonEmptyImages == 2 {
			return trimmed
		}
	}
	return ""
}

func normalizeWan27I2VInput(aliReq *AliVideoRequest, req relaycommon.TaskSubmitReq) error {
	if !isWan27I2VModel(aliReq.Model) {
		return nil
	}

	if len(aliReq.Input.Media) == 0 {
		firstFrameURL := firstNonEmpty(aliReq.Input.FirstFrameURL, aliReq.Input.ImgURL, firstTaskImage(req))
		lastFrameURL := firstNonEmpty(aliReq.Input.LastFrameURL, secondTaskImage(req))
		audioURL := aliReq.Input.AudioURL

		if firstFrameURL != "" {
			aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{
				Type: "first_frame",
				URL:  firstFrameURL,
			})
		}
		if lastFrameURL != "" {
			aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{
				Type: "last_frame",
				URL:  lastFrameURL,
			})
		}
		if audioURL != "" {
			aliReq.Input.Media = append(aliReq.Input.Media, AliVideoMedia{
				Type: "driving_audio",
				URL:  audioURL,
			})
		}
	}

	if len(aliReq.Input.Media) == 0 {
		return fmt.Errorf("wan2.7-i2v requires image, images, input_reference, or input.media")
	}

	// Wan2.7 image-to-video uses the new input.media protocol. Avoid sending
	// legacy fields that belong to wan2.6 and earlier image-to-video APIs.
	aliReq.Input.ImgURL = ""
	aliReq.Input.FirstFrameURL = ""
	aliReq.Input.LastFrameURL = ""
	aliReq.Input.AudioURL = ""
	return nil
}

func (a *TaskAdaptor) convertToAliRequest(info *relaycommon.RelayInfo, req relaycommon.TaskSubmitReq) (*AliVideoRequest, error) {
	upstreamModel := req.Model
	if info != nil && info.ChannelMeta != nil && info.ChannelMeta.UpstreamModelName != "" {
		upstreamModel = info.ChannelMeta.UpstreamModelName
	} else if info != nil && info.ChannelMeta != nil && info.ChannelMeta.IsModelMapped {
		upstreamModel = info.ChannelMeta.UpstreamModelName
	}
	aliReq := &AliVideoRequest{
		Model: upstreamModel,
		Input: AliVideoInput{
			Prompt: req.Prompt,
			ImgURL: firstTaskImage(req),
		},
		Parameters: &AliVideoParameters{
			PromptExtend: common.GetPointer(true), // 默认开启智能改写
			Watermark:    common.GetPointer(false),
		},
	}

	// 处理分辨率映射
	if req.Size != "" {
		// text to video size must be contained *
		if strings.Contains(req.Model, "t2v") && !strings.Contains(req.Size, "*") {
			return nil, fmt.Errorf("invalid size: %s, example: %s", req.Size, "1920*1080")
		}
		if strings.Contains(req.Size, "*") {
			aliReq.Parameters.Size = req.Size
		} else {
			resolution := strings.ToUpper(req.Size)
			// 支持 480p, 720p, 1080p 或 480P, 720P, 1080P
			if !strings.HasSuffix(resolution, "P") {
				resolution = resolution + "P"
			}
			aliReq.Parameters.Resolution = resolution
		}
	} else {
		// 根据模型设置默认分辨率
		if strings.Contains(req.Model, "t2v") { // image to video
			if strings.HasPrefix(req.Model, "wan2.5") {
				aliReq.Parameters.Size = "1920*1080"
			} else if strings.HasPrefix(req.Model, "wan2.2") {
				aliReq.Parameters.Size = "1920*1080"
			} else {
				aliReq.Parameters.Size = "1280*720"
			}
		} else {
			if strings.HasPrefix(req.Model, "wan2.6") {
				aliReq.Parameters.Resolution = "1080P"
			} else if strings.HasPrefix(req.Model, "wan2.5") {
				aliReq.Parameters.Resolution = "1080P"
			} else if strings.HasPrefix(req.Model, "wan2.2-i2v-flash") {
				aliReq.Parameters.Resolution = "720P"
			} else if strings.HasPrefix(req.Model, "wan2.2-i2v-plus") {
				aliReq.Parameters.Resolution = "1080P"
			} else {
				aliReq.Parameters.Resolution = "720P"
			}
		}
	}

	// 处理时长
	if req.Duration > 0 {
		aliReq.Parameters.Duration = common.GetPointer(req.Duration)
	} else if req.Seconds != "" {
		seconds, err := strconv.Atoi(req.Seconds)
		if err != nil {
			return nil, errors.Wrap(err, "convert seconds to int failed")
		} else {
			aliReq.Parameters.Duration = common.GetPointer(seconds)
		}
	} else {
		aliReq.Parameters.Duration = common.GetPointer(5) // 默认5秒
	}

	// 从 metadata 中提取额外参数
	if req.Metadata != nil {
		if metadataBytes, err := common.Marshal(req.Metadata); err == nil {
			err = common.Unmarshal(metadataBytes, aliReq)
			if err != nil {
				return nil, errors.Wrap(err, "unmarshal metadata failed")
			}
		} else {
			return nil, errors.Wrap(err, "marshal metadata failed")
		}
	}

	if err := normalizeAliVideoRequestForModel(info, aliReq, req); err != nil {
		return nil, err
	}

	if aliReq.Model != upstreamModel {
		return nil, errors.New("can't change model with metadata")
	}

	if err := normalizeWan27I2VInput(aliReq, req); err != nil {
		return nil, err
	}

	return aliReq, nil
}

// EstimateBilling 根据用户请求参数计算 OtherRatios（时长、分辨率等）。
// 在 ValidateRequestAndSetAction 之后、价格计算之前调用。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	taskReq, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}

	aliReq, err := a.convertToAliRequest(info, taskReq)
	if err != nil {
		return nil
	}

	// metadata can override Duration past standard request validation;
	// cap it because it is used as a billing multiplier.
	otherRatios := map[string]float64{
		"seconds": float64(intValue(aliReq.Parameters.Duration, 5)),
	}
	ratios, err := ProcessAliOtherRatios(aliReq)
	if err != nil {
		return otherRatios
	}
	for k, v := range ratios {
		otherRatios[k] = v
	}
	return otherRatios
}

// DoRequest delegates to common helper
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response
func (a *TaskAdaptor) ParseResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (parsed *channel.TaskSubmitResponse, taskErr *taskdto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	// 解析阿里响应
	var aliResp AliVideoResponse
	if err := common.Unmarshal(responseBody, &aliResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	// 检查错误
	if aliResp.Code != "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("%s: %s", aliResp.Code, aliResp.Message), "ali_api_error", resp.StatusCode)
		return
	}

	if aliResp.Output.TaskID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	// 转换为 OpenAI 格式响应
	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = info.PublicTaskID
	openAIResp.TaskID = info.PublicTaskID
	openAIResp.Model = c.GetString("model")
	if openAIResp.Model == "" && info != nil {
		openAIResp.Model = info.OriginModelName
	}
	openAIResp.Status = convertAliStatus(aliResp.Output.TaskStatus)
	openAIResp.CreatedAt = common.GetTimestamp()

	// 返回 OpenAI 格式

	return &channel.TaskSubmitResponse{UpstreamTaskID: aliResp.Output.TaskID, TaskData: responseBody, ClientResponse: openAIResp}, nil
}

// FetchTask 查询任务状态
func (a *TaskAdaptor) FetchTask(baseUrl, key string, task *model.Task, proxy string) (*http.Response, error) {
	if task == nil {
		return nil, fmt.Errorf("task is required")
	}
	taskID := task.GetUpstreamTaskID()
	if taskID == "" {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/api/v1/tasks/%s", baseUrl, taskID)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

// ParseTaskResult 解析任务结果
func (a *TaskAdaptor) ParseTaskResult(_ *model.Task, _ *http.Response, respBody []byte) (*relaycommon.TaskInfo, error) {
	var aliResp AliVideoResponse
	if err := common.Unmarshal(respBody, &aliResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{
		Code: 0,
	}

	// 状态映射
	switch aliResp.Output.TaskStatus {
	case "PENDING":
		taskResult.Status = model.TaskStatusQueued
	case "RUNNING":
		taskResult.Status = model.TaskStatusInProgress
	case "SUCCEEDED":
		taskResult.Status = model.TaskStatusSuccess
		// 阿里直接返回视频URL，不需要额外的代理端点
		taskResult.Url = aliResp.Output.VideoURL
	case "FAILED", "CANCELED", "UNKNOWN":
		taskResult.Status = model.TaskStatusFailure
		if aliResp.Message != "" {
			taskResult.Reason = aliResp.Message
		} else if aliResp.Output.Message != "" {
			taskResult.Reason = fmt.Sprintf("task failed, code: %s , message: %s", aliResp.Output.Code, aliResp.Output.Message)
		} else {
			taskResult.Reason = "task failed"
		}
	default:
		taskResult.Status = model.TaskStatusQueued
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	var aliResp AliVideoResponse
	if err := common.Unmarshal(task.Data, &aliResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal ali response failed")
	}

	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = task.TaskID
	openAIResp.Status = convertAliStatus(aliResp.Output.TaskStatus)
	openAIResp.Model = task.Properties.OriginModelName
	openAIResp.SetProgressStr(task.Progress)
	openAIResp.CreatedAt = task.CreatedAt
	openAIResp.CompletedAt = task.UpdatedAt

	// 设置视频URL（核心字段）
	openAIResp.SetMetadata("url", aliResp.Output.VideoURL)

	// 错误处理
	if aliResp.Code != "" {
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    aliResp.Code,
			Message: aliResp.Message,
		}
	} else if aliResp.Output.Code != "" {
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    aliResp.Output.Code,
			Message: aliResp.Output.Message,
		}
	}

	return common.Marshal(openAIResp)
}

func convertAliStatus(aliStatus string) string {
	switch aliStatus {
	case "PENDING":
		return dto.VideoStatusQueued
	case "RUNNING":
		return dto.VideoStatusInProgress
	case "SUCCEEDED":
		return dto.VideoStatusCompleted
	case "FAILED", "CANCELED", "UNKNOWN":
		return dto.VideoStatusFailed
	default:
		return dto.VideoStatusUnknown
	}
}
