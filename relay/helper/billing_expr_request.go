package helper

import (
	"maps"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

func ResolveIncomingBillingExprRequestInput(c *gin.Context, info *relaycommon.RelayInfo) (billingexpr.RequestInput, error) {
	if info != nil && info.BillingRequestInput != nil {
		input := cloneRequestInput(*info.BillingRequestInput)
		merged := cloneStringMap(info.RequestHeaders)
		maps.Copy(merged, input.Headers)
		input.Headers = merged
		return enrichBillingExprRequestInput(info, input), nil
	}

	input := billingexpr.RequestInput{}
	if info != nil {
		input.Headers = cloneStringMap(info.RequestHeaders)
	}

	bodyBytes, err := readIncomingBillingExprBody(c)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	if len(bodyBytes) == 0 {
		bodyBytes, err = readContextBillingExprBody(c)
		if err != nil {
			return billingexpr.RequestInput{}, err
		}
	}
	input.Body = bodyBytes
	input = mergeContextTaskBillingExprRequestInput(c, input)
	return enrichBillingExprRequestInput(info, input), nil
}

// ResolveImageBillingRequestInput freezes only the validated scalar image
// parameters needed by pricing. Image files, prompts and base64 payloads are
// deliberately excluded, including for multipart edits.
func ResolveImageBillingRequestInput(c *gin.Context, info *relaycommon.RelayInfo, input billingexpr.RequestInput) (billingexpr.RequestInput, error) {
	request, ok := info.Request.(*dto.ImageRequest)
	if !ok {
		return input, nil
	}
	count, err := request.ImageCount(false)
	if err != nil {
		return input, err
	}
	body := map[string]any{"model": request.Model, "n": count, "size": request.Size, "quality": request.Quality}
	if request.BillingParameters != nil {
		body["parameters"] = request.BillingParameters
	}
	encoded, err := common.Marshal(body)
	if err != nil {
		return input, err
	}
	input.Body = encoded
	input.ImageCount = &count
	return input, nil
}

func BuildBillingExprRequestInputFromRequest(request dto.Request, headers map[string]string) (billingexpr.RequestInput, error) {
	input := billingexpr.RequestInput{
		Headers: cloneStringMap(headers),
	}
	if request == nil {
		return input, nil
	}

	bodyBytes, err := common.Marshal(request)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	input.Body = bodyBytes
	return input, nil
}

func readIncomingBillingExprBody(c *gin.Context) ([]byte, error) {
	if c == nil || c.Request == nil {
		return nil, nil
	}
	contentType := c.Request.Header.Get("Content-Type")
	if isMultipartFormContentType(contentType) {
		return readIncomingBillingExprMultipartBody(c)
	}
	if !isJSONContentType(contentType) {
		return nil, nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	return storage.Bytes()
}

func readIncomingBillingExprMultipartBody(c *gin.Context) ([]byte, error) {
	form := c.Request.MultipartForm
	if form == nil {
		parsedForm, err := common.ParseMultipartFormReusable(c)
		if err != nil {
			return nil, err
		}
		form = parsedForm
		c.Request.MultipartForm = parsedForm
	}
	if form == nil {
		return nil, nil
	}

	body := map[string]any{}
	for key, values := range form.Value {
		setBillingExprFormValue(body, key, values)
	}

	files := map[string]any{}
	fileCount := 0
	var totalFileSize int64
	for key, fileHeaders := range form.File {
		key = strings.TrimSpace(key)
		if key == "" || len(fileHeaders) == 0 {
			continue
		}

		items := make([]map[string]any, 0, len(fileHeaders))
		for _, fileHeader := range fileHeaders {
			if fileHeader == nil {
				continue
			}
			item := map[string]any{
				"filename": fileHeader.Filename,
				"size":     fileHeader.Size,
			}
			if contentType := strings.TrimSpace(fileHeader.Header.Get("Content-Type")); contentType != "" {
				item["content_type"] = contentType
			}
			items = append(items, item)
			fileCount++
			totalFileSize += fileHeader.Size
		}
		if len(items) == 0 {
			continue
		}
		if len(items) == 1 {
			files[key] = items[0]
		} else {
			files[key] = items
		}
		if key == "file" {
			body["file_name"] = items[0]["filename"]
			body["file_size"] = items[0]["size"]
			if contentType, ok := items[0]["content_type"]; ok {
				body["file_content_type"] = contentType
			}
		}
	}
	if len(files) > 0 {
		body["files"] = files
		body["file_count"] = fileCount
		body["file_size_total"] = totalFileSize
	}

	return common.Marshal(body)
}

func readContextBillingExprBody(c *gin.Context) ([]byte, error) {
	if c == nil {
		return nil, nil
	}
	if value, ok := c.Get("task_request"); ok && value != nil {
		return common.Marshal(value)
	}
	return nil, nil
}

func mergeContextTaskBillingExprRequestInput(c *gin.Context, input billingexpr.RequestInput) billingexpr.RequestInput {
	if c == nil {
		return input
	}
	value, ok := c.Get("task_request")
	if !ok || value == nil {
		return input
	}
	taskReq, ok := value.(relaycommon.TaskSubmitReq)
	body := map[string]any{}
	if len(input.Body) > 0 {
		_ = common.Unmarshal(input.Body, &body)
	}
	if !ok {
		// Protocol plugins retain their canonical request as a map. Merge it
		// before normalizing the fork's existing duration/resolution probes.
		if taskBody, valid := value.(map[string]any); valid {
			maps.Copy(body, taskBody)
		} else {
			return input
		}
	}
	if strings.TrimSpace(taskReq.Model) != "" {
		body["model"] = taskReq.Model
	}
	if strings.TrimSpace(taskReq.Size) != "" {
		body["size"] = taskReq.Size
	}
	if taskReq.Duration > 0 {
		body["duration"] = taskReq.Duration
	}
	if strings.TrimSpace(taskReq.Seconds) != "" {
		body["seconds"] = taskReq.Seconds
	}
	if len(taskReq.Metadata) > 0 {
		body["metadata"] = taskReq.Metadata
	}
	if resolution := resolveTaskBillingResolution(taskReq, body); resolution != "" {
		body["resolution"] = resolution
	}

	bodyBytes, err := common.Marshal(body)
	if err != nil {
		return input
	}
	input.Body = bodyBytes
	return input
}

func resolveTaskBillingResolution(taskReq relaycommon.TaskSubmitReq, body map[string]any) string {
	for _, candidate := range []any{
		body["resolution"],
		body["size"],
		taskReq.Metadata["resolution"],
		billingExprMapValue(body["metadata"], "resolution"),
		taskReq.Size,
		taskReq.Metadata["size"],
		billingExprMapValue(body["metadata"], "size"),
	} {
		if resolution := normalizeTaskBillingResolution(candidate); resolution != "" {
			return resolution
		}
	}
	return ""
}

func billingExprMapValue(value any, key string) any {
	switch m := value.(type) {
	case map[string]any:
		return m[key]
	case string:
		var parsed map[string]any
		if err := common.UnmarshalJsonStr(m, &parsed); err == nil {
			return parsed[key]
		}
		return nil
	default:
		return nil
	}
}

func normalizeTaskBillingResolution(value any) string {
	text := strings.ToLower(strings.TrimSpace(common.Interface2String(value)))
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "_", "")
	text = strings.ReplaceAll(text, " ", "")
	if resolution := exactTaskBillingResolution(text); resolution != "" {
		return resolution
	}
	if strings.Contains(text, "4k") || strings.Contains(text, "2160p") {
		return "4k"
	}
	if strings.Contains(text, "1080p") {
		return "1080p"
	}
	if strings.Contains(text, "768p") {
		return "720p"
	}
	if strings.Contains(text, "720p") {
		return "720p"
	}
	if strings.Contains(text, "512p") {
		return "480p"
	}
	if strings.Contains(text, "480p") {
		return "480p"
	}
	if maxDimension := maxVideoDimension(text); maxDimension > 0 {
		switch {
		case maxDimension >= 3840:
			return "4k"
		case maxDimension >= 1600:
			return "1080p"
		case maxDimension >= 1000:
			return "720p"
		default:
			return "480p"
		}
	}
	switch {
	case strings.Contains(text, "2160"):
		return "4k"
	case strings.Contains(text, "1080"):
		return "1080p"
	case strings.Contains(text, "768"):
		return "720p"
	case strings.Contains(text, "720"):
		return "720p"
	case strings.Contains(text, "512"):
		return "480p"
	case strings.Contains(text, "480"):
		return "480p"
	default:
		return ""
	}
}

func exactTaskBillingResolution(value string) string {
	normalized := strings.ReplaceAll(value, "*", "x")
	switch normalized {
	case "4k", "2160p", "2160", "3840x2160", "2160x3840", "4096x2160":
		return "4k"
	case "1080p", "1080", "1920x1080", "1080x1920", "1792x1024", "1024x1792",
		"1440x1440", "1632x1248", "1248x1632":
		return "1080p"
	case "720p", "720", "768p", "768", "1280x720", "720x1280", "960x960",
		"1088x832", "832x1088":
		return "720p"
	case "480p", "480", "512p", "512", "854x480", "480x854", "832x480",
		"480x832", "624x624":
		return "480p"
	default:
		return ""
	}
}

func maxVideoDimension(value string) int {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == 'x' || r == '*' || r == ',' || r == '/'
	})
	maxDimension := 0
	for _, part := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		if n > maxDimension {
			maxDimension = n
		}
	}
	return maxDimension
}

func enrichBillingExprRequestInput(info *relaycommon.RelayInfo, input billingexpr.RequestInput) billingexpr.RequestInput {
	if info == nil || info.RelayMode != relayconstant.RelayModeAudioSpeech || len(input.Body) == 0 {
		return input
	}

	body := map[string]any{}
	if err := common.Unmarshal(input.Body, &body); err != nil {
		return input
	}
	if positiveBillingExprNumber(body["seconds"]) ||
		positiveBillingExprNumber(body["duration"]) ||
		positiveBillingExprNumber(body["estimated_duration"]) {
		return input
	}

	text, ok := body["input"].(string)
	if !ok || strings.TrimSpace(text) == "" {
		return input
	}
	estimatedSeconds := estimateAudioSpeechDurationSeconds(text, billingExprNumber(body["speed"]))
	if estimatedSeconds <= 0 {
		return input
	}

	body["estimated_duration"] = estimatedSeconds
	if bodyBytes, err := common.Marshal(body); err == nil {
		input.Body = bodyBytes
	}
	return input
}

func cloneRequestInput(src billingexpr.RequestInput) billingexpr.RequestInput {
	input := billingexpr.RequestInput{
		Headers: cloneStringMap(src.Headers),
	}
	if src.ImageCount != nil {
		count := *src.ImageCount
		input.ImageCount = &count
	}
	if len(src.Body) > 0 {
		input.Body = append([]byte(nil), src.Body...)
	}
	return input
}

func isJSONContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(contentType, "application/json")
}

func isMultipartFormContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(contentType, "multipart/form-data")
}

func setBillingExprFormValue(body map[string]any, key string, values []string) {
	key = strings.TrimSpace(key)
	if key == "" || len(values) == 0 {
		return
	}
	if len(values) == 1 {
		body[key] = values[0]
		return
	}
	body[key] = append([]string(nil), values...)
}

func positiveBillingExprNumber(value any) bool {
	return billingExprNumber(value) > 0
}

func billingExprNumber(value any) float64 {
	switch v := value.(type) {
	case nil:
		return 0
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0
		}
		return v
	case float32:
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0
		}
		return f
	case int:
		return float64(v)
	case int8:
		return float64(v)
	case int16:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case uint:
		return float64(v)
	case uint8:
		return float64(v)
	case uint16:
		return float64(v)
	case uint32:
		return float64(v)
	case uint64:
		return float64(v)
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0
		}
		return f
	default:
		return 0
	}
}

func estimateAudioSpeechDurationSeconds(text string, speed float64) float64 {
	normalized := strings.Join(strings.Fields(text), " ")
	if normalized == "" {
		return 0
	}

	cjkChars := 0
	otherChars := 0
	words := 0
	inWord := false
	for _, r := range normalized {
		if isCJKRune(r) {
			cjkChars++
			if inWord {
				words++
				inWord = false
			}
			continue
		}
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if inWord {
				words++
				inWord = false
			}
			continue
		}
		otherChars++
		inWord = true
	}
	if inWord {
		words++
	}

	estimated := float64(cjkChars)/4.5 + math.Max(float64(words)/2.6, float64(otherChars)/14)
	if speed <= 0 {
		speed = 1
	}
	return math.Max(1, math.Ceil(estimated/speed))
}

func isCJKRune(r rune) bool {
	return (r >= 0x3400 && r <= 0x9fff) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0x20000 && r <= 0x2ebef)
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		if strings.TrimSpace(key) == "" {
			continue
		}
		dst[key] = value
	}
	return dst
}
