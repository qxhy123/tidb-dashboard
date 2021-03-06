// Copyright 2020 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package profiling

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/pingcap-incubator/tidb-dashboard/pkg/apiserver/user"
	"github.com/pingcap-incubator/tidb-dashboard/pkg/apiserver/utils"
	"github.com/pingcap-incubator/tidb-dashboard/pkg/config"
)

// Register register the handlers to the service.
func Register(r *gin.RouterGroup, auth *user.AuthService, s *Service) {
	endpoint := r.Group("/profiling")
	endpoint.GET("/group/list", auth.MWAuthRequired(), s.getGroupList)
	endpoint.POST("/group/start", auth.MWAuthRequired(), s.handleStartGroup)
	endpoint.GET("/group/detail/:groupId", auth.MWAuthRequired(), s.getGroupDetail)
	endpoint.POST("/group/cancel/:groupId", auth.MWAuthRequired(), s.handleCancelGroup)
	endpoint.DELETE("/group/delete/:groupId", auth.MWAuthRequired(), s.deleteGroup)
	endpoint.GET("/group/download/acquire_token", auth.MWAuthRequired(), s.getGroupDownloadToken)
	endpoint.GET("/group/download", s.downloadGroup)
	endpoint.GET("/single/download/acquire_token", auth.MWAuthRequired(), s.getSingleDownloadToken)
	endpoint.GET("/single/download", s.downloadSingle)
	endpoint.GET("/config", s.getDynamicConfig)
	endpoint.PUT("/config", s.setDynamicConfig)
}

// @ID startProfiling
// @Summary Start profiling
// @Description Start a profiling task group
// @Produce json
// @Param req body StartRequest true "profiling request"
// @Security JwtAuth
// @Success 200 {object} TaskGroupModel "task group"
// @Failure 400 {object} utils.APIError
// @Failure 401 {object} utils.APIError "Unauthorized failure"
// @Failure 500 {object} utils.APIError
// @Router /profiling/group/start [post]
func (s *Service) handleStartGroup(c *gin.Context) {
	var req StartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Status(http.StatusBadRequest)
		_ = c.Error(err)
		return
	}
	if len(req.Targets) == 0 {
		c.Status(http.StatusBadRequest)
		_ = c.Error(utils.ErrInvalidRequest.NewWithNoMessage())
		return
	}

	if req.DurationSecs == 0 {
		req.DurationSecs = config.DefaultProfilingAutoCollectionDurationSecs
	}
	if req.DurationSecs > config.MaxProfilingAutoCollectionDurationSecs {
		req.DurationSecs = config.MaxProfilingAutoCollectionDurationSecs
	}

	session := &StartRequestSession{
		req: req,
		ch:  make(chan struct{}, 1),
	}
	s.sessionCh <- session
	select {
	case <-session.ch:
		if session.err != nil {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(session.err)
		} else {
			c.JSON(http.StatusOK, session.taskGroup.TaskGroupModel)
		}
	case <-time.After(Timeout):
		c.Status(http.StatusInternalServerError)
		_ = c.Error(ErrTimeout.NewWithNoMessage())
	}
}

// @ID getProfilingGroups
// @Summary List all profiling groups
// @Description List all profiling groups
// @Produce json
// @Security JwtAuth
// @Success 200 {array} TaskGroupModel
// @Failure 401 {object} utils.APIError "Unauthorized failure"
// @Router /profiling/group/list [get]
func (s *Service) getGroupList(c *gin.Context) {
	var resp []TaskGroupModel
	err := s.db.Order("id DESC").Find(&resp).Error
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

type GroupDetailResponse struct {
	ServerTime int64          `json:"server_time"`
	TaskGroup  TaskGroupModel `json:"task_group_status"`
	Tasks      []TaskModel    `json:"tasks_status"`
}

// @ID getProfilingGroupDetail
// @Summary List all tasks with a given group ID
// @Description List all profiling tasks with a given group ID
// @Produce json
// @Param groupId path string true "group ID"
// @Security JwtAuth
// @Success 200 {object} GroupDetailResponse
// @Failure 400 {object} utils.APIError
// @Failure 401 {object} utils.APIError "Unauthorized failure"
// @Router /profiling/group/detail/{groupId} [get]
func (s *Service) getGroupDetail(c *gin.Context) {
	taskGroupID, err := strconv.Atoi(c.Param("groupId"))
	if err != nil {
		c.Status(http.StatusBadRequest)
		_ = c.Error(err)
		return
	}
	var taskGroup TaskGroupModel
	err = s.db.Where("id = ?", taskGroupID).Find(&taskGroup).Error
	if err != nil {
		c.Status(http.StatusBadRequest)
		_ = c.Error(err)
		return
	}

	var tasks []TaskModel
	err = s.db.Where("task_group_id = ?", taskGroupID).Find(&tasks).Error
	if err != nil {
		c.Status(http.StatusBadRequest)
		_ = c.Error(err)
		return
	}

	c.JSON(http.StatusOK, GroupDetailResponse{
		ServerTime: time.Now().Unix(), // Used to estimate task progress
		TaskGroup:  taskGroup,
		Tasks:      tasks,
	})
}

// @ID cancelProfilingGroup
// @Summary Cancel all tasks with a given group ID
// @Description Cancel all profling tasks with a given group ID
// @Produce json
// @Param groupId path string true "group ID"
// @Security JwtAuth
// @Success 200 {object} utils.APIEmptyResponse
// @Failure 400 {object} utils.APIError
// @Failure 401 {object} utils.APIError "Unauthorized failure"
// @Router /profiling/group/cancel/{groupId} [post]
func (s *Service) handleCancelGroup(c *gin.Context) {
	taskGroupID, err := strconv.Atoi(c.Param("groupId"))
	if err != nil {
		c.Status(http.StatusBadRequest)
		_ = c.Error(err)
		return
	}
	if err := s.cancelGroup(uint(taskGroupID)); err != nil {
		c.Status(http.StatusBadRequest)
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, utils.APIEmptyResponse{})
}

// @ID getProfilingGroupDownloadToken
// @Summary Get download token for group download
// @Description Get download token with a given group ID
// @Produce plain
// @Param id query string false "group ID"
// @Security JwtAuth
// @Success 200 {string} string
// @Failure 400 {object} utils.APIError
// @Failure 401 {object} utils.APIError "Unauthorized failure"
// @Router /profiling/group/download/acquire_token [get]
func (s *Service) getGroupDownloadToken(c *gin.Context) {
	id := c.Query("id")
	token, err := utils.NewJWTString("profiling/group_download", id)
	if err != nil {
		c.Status(http.StatusBadRequest)
		_ = c.Error(utils.ErrInvalidRequest.WrapWithNoMessage(err))
		return
	}
	c.String(http.StatusOK, token)
}

// @ID downloadProfilingGroup
// @Summary Download all results of a task group
// @Description Download all finished profiling results of a task group
// @Produce application/x-gzip
// @Param token query string true "download token"
// @Security JwtAuth
// @Failure 400 {object} utils.APIError
// @Failure 401 {object} utils.APIError "Unauthorized failure"
// @Failure 500 {object} utils.APIError
// @Router /profiling/group/download [get]
func (s *Service) downloadGroup(c *gin.Context) {
	token := c.Query("token")
	str, err := utils.ParseJWTString("profiling/group_download", token)
	if err != nil {
		c.Status(http.StatusUnauthorized)
		_ = c.Error(utils.ErrInvalidRequest.New(err.Error()))
		return
	}
	taskGroupID, err := strconv.Atoi(str)
	if err != nil {
		c.Status(http.StatusBadRequest)
		_ = c.Error(err)
		return
	}
	var tasks []TaskModel
	err = s.db.Where("task_group_id = ? AND state = ?", taskGroupID, TaskStateFinish).Find(&tasks).Error
	if err != nil {
		c.Status(http.StatusBadRequest)
		_ = c.Error(err)
		return
	}

	filePathes := make([]string, len(tasks))
	for i, task := range tasks {
		filePathes[i] = task.FilePath
	}

	temp, err := ioutil.TempFile("", fmt.Sprintf("taskgroup_%d", taskGroupID))
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(err)
		return
	}

	err = createTarball(temp, filePathes)
	defer temp.Close()
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(err)
		return
	}

	fileName := fmt.Sprintf("profiling_pack_%d.tar.gz", taskGroupID)
	c.FileAttachment(temp.Name(), fileName)
}

// @ID getProfilingSingleDownloadToken
// @Summary Get download token for single download
// @Description Get download token with a given task ID
// @Produce plain
// @Param id query string false "task ID"
// @Security JwtAuth
// @Success 200 {string} string
// @Failure 400 {object} utils.APIError
// @Failure 401 {object} utils.APIError "Unauthorized failure"
// @Router /profiling/single/download/acquire_token [get]
func (s *Service) getSingleDownloadToken(c *gin.Context) {
	id := c.Query("id")
	token, err := utils.NewJWTString("profiling/single_download", id)
	if err != nil {
		c.Status(http.StatusBadRequest)
		_ = c.Error(utils.ErrInvalidRequest.WrapWithNoMessage(err))
		return
	}
	c.String(http.StatusOK, token)
}

// @ID downloadProfilingSingle
// @Summary Download the result of a task
// @Description Download the finished profiling result of a task
// @Produce application/x-gzip
// @Param token query string true "download token"
// @Security JwtAuth
// @Failure 400 {object} utils.APIError
// @Failure 401 {object} utils.APIError "Unauthorized failure"
// @Failure 500 {object} utils.APIError
// @Router /profiling/single/download [get]
func (s *Service) downloadSingle(c *gin.Context) {
	// FIXME: We can simply provide only a single file
	token := c.Query("token")
	str, err := utils.ParseJWTString("profiling/single_download", token)
	if err != nil {
		c.Status(http.StatusUnauthorized)
		_ = c.Error(utils.ErrInvalidRequest.New(err.Error()))
		return
	}
	taskID, err := strconv.Atoi(str)
	if err != nil {
		c.Status(http.StatusBadRequest)
		_ = c.Error(err)
		return
	}
	task := TaskModel{}
	err = s.db.Where("id = ? AND state = ?", taskID, TaskStateFinish).First(&task).Error
	if err != nil {
		c.Status(http.StatusBadRequest)
		_ = c.Error(err)
		return
	}

	temp, err := ioutil.TempFile("", fmt.Sprintf("task_%d", taskID))
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(err)
		return
	}

	err = createTarball(temp, []string{task.FilePath})
	defer temp.Close()
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(err)
		return
	}

	fileName := fmt.Sprintf("profiling_%d.tar.gz", taskID)
	c.FileAttachment(temp.Name(), fileName)
}

// @ID deleteProfilingGroup
// @Summary Delete all tasks with a given group ID
// @Description Delete all finished profiling tasks with a given group ID
// @Produce json
// @Param groupId path string true "group ID"
// @Security JwtAuth
// @Success 200 {object} utils.APIEmptyResponse
// @Failure 400 {object} utils.APIError
// @Failure 401 {object} utils.APIError "Unauthorized failure"
// @Failure 500 {object} utils.APIError
// @Router /profiling/group/delete/{groupId} [delete]
func (s *Service) deleteGroup(c *gin.Context) {
	taskGroupID, err := strconv.Atoi(c.Param("groupId"))
	if err != nil {
		c.Status(http.StatusBadRequest)
		_ = c.Error(err)
		return
	}
	if err := s.cancelGroup(uint(taskGroupID)); err != nil {
		c.Status(http.StatusBadRequest)
		_ = c.Error(err)
		return
	}

	if err = s.db.Where("task_group_id = ?", taskGroupID).Delete(&TaskModel{}).Error; err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(err)
		return
	}
	if err = s.db.Where("id = ?", taskGroupID).Delete(&TaskGroupModel{}).Error; err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, utils.APIEmptyResponse{})
}

// @Summary Get Profiling Dynamic Config
// @Produce json
// @Success 200 {object} config.ProfilingConfig
// @Router /profiling/config [get]
// @Security JwtAuth
// @Failure 401 {object} utils.APIError "Unauthorized failure"
func (s *Service) getDynamicConfig(c *gin.Context) {
	c.JSON(http.StatusOK, s.cfgManager.Get().Profiling)
}

// @Summary Set Profiling Dynamic Config
// @Produce json
// @Param request body config.ProfilingConfig true "Request body"
// @Success 200 {object} config.ProfilingConfig
// @Router /profiling/config [put]
// @Security JwtAuth
// @Failure 400 {object} utils.APIError
// @Failure 401 {object} utils.APIError "Unauthorized failure"
// @Failure 500 {object} utils.APIError
func (s *Service) setDynamicConfig(c *gin.Context) {
	var req config.ProfilingConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Status(http.StatusBadRequest)
		_ = c.Error(utils.ErrInvalidRequest.WrapWithNoMessage(err))
		return
	}
	var opt config.DynamicConfigOption = func(dc *config.DynamicConfig) {
		dc.Profiling = req
	}
	if err := s.cfgManager.Set(opt); err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(utils.ErrInvalidRequest.WrapWithNoMessage(err))
		return
	}
	c.JSON(http.StatusOK, req)
}
