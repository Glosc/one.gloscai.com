package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

type agentPlanVideoTaskClient struct {
	channel *model.Channel
	adaptor relaychannel.TaskAdaptor
	key     string
}

func newAgentPlanVideoTaskClient(channelID int) (*agentPlanVideoTaskClient, error) {
	channelModel, err := model.GetChannelById(channelID, true)
	if err != nil {
		return nil, err
	}
	if channelModel.Type != constant.ChannelTypeVolcEnginePlan {
		return nil, fmt.Errorf("channel %d is not a VolcEngine Agent Plan channel", channelID)
	}
	if channelModel.ChannelInfo.IsMultiKey {
		return nil, fmt.Errorf("multi-key Agent Plan channels are not supported for video task operations")
	}

	key, _, apiErr := channelModel.GetNextEnabledKey()
	if apiErr != nil {
		return nil, apiErr
	}
	adaptor := relay.GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(channelModel.Type)))
	if adaptor == nil {
		return nil, fmt.Errorf("VolcEngine Agent Plan video adaptor is unavailable")
	}
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelType:    channelModel.Type,
		ChannelBaseUrl: channelModel.GetBaseURL(),
		ApiKey:         key,
		ChannelSetting: channelModel.GetSetting(),
	}})
	return &agentPlanVideoTaskClient{
		channel: channelModel,
		adaptor: adaptor,
		key:     key,
	}, nil
}

func proxyAgentPlanVideoTaskResponse(c *gin.Context, resp *http.Response) {
	if requestID := resp.Header.Get("X-Request-Id"); requestID != "" {
		c.Header("X-Request-Id", requestID)
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	c.DataFromReader(resp.StatusCode, resp.ContentLength, contentType, resp.Body, nil)
}

func ListAgentPlanVideoTasks(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid channel id"})
		return
	}
	client, err := newAgentPlanVideoTaskClient(channelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	lister, ok := client.adaptor.(relaychannel.TaskListAdaptor)
	if !ok {
		c.JSON(http.StatusNotImplemented, gin.H{"success": false, "message": "video task list is not supported"})
		return
	}

	params := relaychannel.TaskListParams{
		Status:      strings.TrimSpace(c.Query("filter.status")),
		Model:       strings.TrimSpace(c.Query("filter.model")),
		ServiceTier: strings.TrimSpace(c.Query("filter.service_tier")),
	}
	for _, pagination := range []struct {
		name   string
		target *int
	}{
		{name: "page_num", target: &params.PageNum},
		{name: "page_size", target: &params.PageSize},
	} {
		rawValue := strings.TrimSpace(c.Query(pagination.name))
		if rawValue == "" {
			continue
		}
		value, parseErr := strconv.ParseInt(rawValue, 10, 32)
		if parseErr != nil || value <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": pagination.name + " must be a positive integer"})
			return
		}
		*pagination.target = int(value)
	}
	for _, taskIDGroup := range c.QueryArray("filter.task_ids") {
		for _, taskID := range strings.Split(taskIDGroup, ",") {
			if taskID = strings.TrimSpace(taskID); taskID != "" {
				params.TaskIDs = append(params.TaskIDs, taskID)
			}
		}
	}

	resp, err := lister.FetchTaskList(client.channel.GetBaseURL(), client.key, params, client.channel.GetSetting().Proxy)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": err.Error()})
		return
	}
	defer resp.Body.Close()
	proxyAgentPlanVideoTaskResponse(c, resp)
}

func GetAgentPlanVideoTask(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid channel id"})
		return
	}
	client, err := newAgentPlanVideoTaskClient(channelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	resp, err := client.adaptor.FetchTask(client.channel.GetBaseURL(), client.key, &model.Task{TaskID: c.Param("task_id")}, client.channel.GetSetting().Proxy)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": err.Error()})
		return
	}
	defer resp.Body.Close()
	proxyAgentPlanVideoTaskResponse(c, resp)
}

func DeleteAgentPlanVideoTask(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid channel id"})
		return
	}
	client, err := newAgentPlanVideoTaskClient(channelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	deleter, ok := client.adaptor.(relaychannel.TaskDeleteAdaptor)
	if !ok {
		c.JSON(http.StatusNotImplemented, gin.H{"success": false, "message": "video task deletion is not supported"})
		return
	}

	resp, err := deleter.DeleteTask(client.channel.GetBaseURL(), client.key, c.Param("task_id"), client.channel.GetSetting().Proxy)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": err.Error()})
		return
	}
	defer resp.Body.Close()
	proxyAgentPlanVideoTaskResponse(c, resp)
}
