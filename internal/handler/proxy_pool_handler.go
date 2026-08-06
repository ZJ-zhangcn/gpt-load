package handler

import (
	"io"
	"strconv"

	app_errors "gpt-load/internal/errors"
	"gpt-load/internal/response"

	"github.com/gin-gonic/gin"
)

type proxyImportRequest struct {
	ProxiesText string `json:"proxies_text" binding:"required"`
}

type proxyRebalanceRequest struct {
	GroupID  uint   `json:"group_id" binding:"required"`
	ProxyIDs []uint `json:"proxy_ids" binding:"required,min=1"`
}

type proxyCheckRequest struct {
	ProxyIDs []uint `json:"proxy_ids"`
}

type proxyBatchDeleteRequest struct {
	ProxyIDs []uint `json:"proxy_ids" binding:"required,min=1"`
}

// ListProxies returns the current proxy node pool for the authenticated management UI.
func (s *Server) ListProxies(c *gin.Context) {
	proxies, err := s.ProxyPoolService.List()
	if err != nil {
		response.Error(c, app_errors.ParseDBError(err))
		return
	}
	response.Success(c, proxies)
}

// CheckProxies performs a real outbound probe through the selected nodes. An
// empty proxy_ids list checks the complete pool.
func (s *Server) CheckProxies(c *gin.Context) {
	var req proxyCheckRequest
	if err := c.ShouldBindJSON(&req); err != nil && err != io.EOF {
		response.Error(c, app_errors.ErrInvalidJSON)
		return
	}
	result, err := s.ProxyPoolService.Check(c.Request.Context(), req.ProxyIDs)
	if err != nil {
		response.Error(c, app_errors.NewAPIError(app_errors.ErrBadRequest, err.Error()))
		return
	}
	response.Success(c, result)
}

// ImportProxies accepts a newline/comma/semicolon-separated node list.
func (s *Server) ImportProxies(c *gin.Context) {
	var req proxyImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, app_errors.ErrInvalidJSON)
		return
	}
	result, err := s.ProxyPoolService.Import(req.ProxiesText)
	if err != nil {
		response.Error(c, app_errors.NewAPIError(app_errors.ErrBadRequest, err.Error()))
		return
	}
	response.Success(c, result)
}

// RebalanceProxies assigns selected pool nodes to all keys in one standard group.
func (s *Server) RebalanceProxies(c *gin.Context) {
	var req proxyRebalanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, app_errors.ErrInvalidJSON)
		return
	}
	result, err := s.ProxyPoolService.Rebalance(req.GroupID, req.ProxyIDs)
	if err != nil {
		response.Error(c, app_errors.NewAPIError(app_errors.ErrBadRequest, err.Error()))
		return
	}
	response.Success(c, result)
}

// RebalanceAllProxies assigns every healthy pool node across all standard groups in one transaction.
func (s *Server) RebalanceAllProxies(c *gin.Context) {
	result, err := s.ProxyPoolService.RebalanceAllHealthy()
	if err != nil {
		response.Error(c, app_errors.NewAPIError(app_errors.ErrBadRequest, err.Error()))
		return
	}
	response.Success(c, result)
}

// DeleteProxy deletes a pool node and atomically clears every key that referenced it.
func (s *Server) DeleteProxy(c *gin.Context) {
	proxyID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || proxyID == 0 {
		response.Error(c, app_errors.ErrBadRequest)
		return
	}
	result, err := s.ProxyPoolService.Delete(uint(proxyID))
	if err != nil {
		response.Error(c, app_errors.ParseDBError(err))
		return
	}
	response.Success(c, result)
}

// DeleteProxies physically deletes selected pool nodes in one transaction.
func (s *Server) DeleteProxies(c *gin.Context) {
	var req proxyBatchDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, app_errors.ErrInvalidJSON)
		return
	}
	result, err := s.ProxyPoolService.DeleteMany(req.ProxyIDs)
	if err != nil {
		response.Error(c, app_errors.NewAPIError(app_errors.ErrBadRequest, err.Error()))
		return
	}
	response.Success(c, result)
}
