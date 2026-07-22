package rules

import (
	"github.com/robfig/cron/v3"
)

// Scheduler 把 schedule 触发器规则挂到 cron 上，触发时回调宿主执行 do。
type Scheduler struct {
	c *cron.Cron
}

// NewScheduler 创建调度器；fire 在规则到点时被调用（宿主负责执行 do 动作）。
func NewScheduler(all []*Rule, fire func(r *Rule)) (*Scheduler, error) {
	c := cron.New() // 标准 5 段 cron
	for _, r := range all {
		if !r.Enabled || r.When.Type() != "schedule" {
			continue
		}
		rule := r
		if _, err := c.AddFunc(rule.When.Cron, func() { fire(rule) }); err != nil {
			return nil, err
		}
	}
	return &Scheduler{c: c}, nil
}

// Start 启动调度。
func (s *Scheduler) Start() { s.c.Start() }

// Stop 停止调度（reload 时换新实例）。
func (s *Scheduler) Stop() { s.c.Stop() }

// ValidateCron 校验 cron 表达式（保存规则时用）。
func ValidateCron(expr string) error {
	_, err := cron.ParseStandard(expr)
	return err
}
