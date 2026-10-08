package tools

import (
	"context"
	"fmt"
	"log"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/google/uuid"
)

// ToolNameOrderMilkTea 是下单工具名。
const ToolNameOrderMilkTea = "order_milk_tea"

// orderMilkTeaArgs 是下单工具的入参。
type orderMilkTeaArgs struct {
	Flavor string `json:"flavor" jsonschema:"description=奶茶口味，如 芋泥波波 / 四季春"`
	Size   string `json:"size" jsonschema:"description=杯型,enum=中杯,enum=大杯"`
	Sugar  string `json:"sugar" jsonschema:"description=甜度,enum=无糖,enum=三分糖,enum=五分糖,enum=全糖"`
	Count  int    `json:"count" jsonschema:"description=杯数，至少 1"`
}

// orderMilkTeaResult 是下单工具的返回值。
//
// 用户取消也走这个结构（Cancelled=true、OrderID 为空），而不是返回 error：
// 工具返回 error 会被引擎当作节点失败，整个 run 以 RUN_ERROR 收场，模型没机会
// 向用户解释。取消是正常业务结果，必须让模型拿到并复述。
type orderMilkTeaResult struct {
	Cancelled bool   `json:"cancelled,omitempty"`
	Message   string `json:"message,omitempty"`
	OrderID   string `json:"orderId,omitempty"`
	Flavor    string `json:"flavor,omitempty"`
	Size      string `json:"size,omitempty"`
	Sugar     string `json:"sugar,omitempty"`
	Count     int    `json:"count,omitempty"`
	Amount    int    `json:"amount,omitempty"` // 金额，单位：分
	ETA       string `json:"eta,omitempty"`    // 预计送达时间
}

// unitPriceCents 单杯价格（分）。订单落库是后续里程碑，这里先用固定价。
const unitPriceCents = 1800

// NewOrderMilkTeaTool 构造下单工具。
//
// 工具带下单前审批：首次调用不下单，而是 compose.Interrupt 把订单详情抛给用户确认；
// 用户批准后 TurnLoop 恢复执行，此时 compose.GetResumeContext[bool] 读到 true 才真正落单，
// 读到 false 视为用户取消。
func NewOrderMilkTeaTool() tool.InvokableTool {
	t, err := utils.InferTool(ToolNameOrderMilkTea, "下单购买奶茶。",
		func(ctx context.Context, args *orderMilkTeaArgs) (*orderMilkTeaResult, error) {
			if args == nil {
				return nil, fmt.Errorf("下单参数为空")
			}
			count := args.Count
			if count <= 0 {
				count = 1
			}

			// 恢复路径：hasData 为 false 说明用户只是"继续"而没给决定，按未批准处理。
			if isResume, hasData, approved := compose.GetResumeContext[bool](ctx); isResume {
				if !hasData || !approved {
					return &orderMilkTeaResult{
						Cancelled: true,
						Message:   "用户取消了本次下单，订单未创建。",
					}, nil
				}
				return &orderMilkTeaResult{
					OrderID: uuid.New().String(),
					Flavor:  args.Flavor,
					Size:    args.Size,
					Sugar:   args.Sugar,
					Count:   count,
					Amount:  count * unitPriceCents,
					ETA:     "15 分钟",
				}, nil
			}

			// 首次路径：中断等待确认。返回的 error 必须原样透传，不能包裹，
			// 否则引擎识别不出这是 interrupt 信号。
			return nil, compose.Interrupt(ctx, fmt.Sprintf(
				"确认下单 %d 杯%s（%s / %s），合计 %.2f 元？",
				count, args.Flavor, args.Size, args.Sugar,
				float64(count*unitPriceCents)/100,
			))
		})
	if err != nil {
		log.Printf("[tools] 构建 %s 工具失败: %v", ToolNameOrderMilkTea, err)
		return nil
	}
	return t
}
