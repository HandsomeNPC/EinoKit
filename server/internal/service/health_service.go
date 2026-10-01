// Package service 业务逻辑层，与 HTTP 解耦。
package service

// HealthResult 健康检查结果。
type HealthResult struct {
	Status string `json:"status"`
}

// HealthService 健康检查业务逻辑。
type HealthService struct{}

// HealthSvcApp 单例，供 handler 引用。
var HealthSvcApp = new(HealthService)

// Check 返回服务健康状态。
func (s *HealthService) Check() HealthResult {
	return HealthResult{Status: "ok"}
}
