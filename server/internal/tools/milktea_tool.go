package tools

import (
	"context"
	"fmt"
	"log"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
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
type orderMilkTeaResult struct {
	OrderID string `json:"orderId"`
	Flavor  string `json:"flavor"`
	Size    string `json:"size"`
	Sugar   string `json:"sugar"`
	Count   int    `json:"count"`
	Amount  int    `json:"amount"` // 金额，单位：分
	ETA     string `json:"eta"`    // 预计送达时间
}

// unitPriceCents 单杯价格（分）。订单落库是后续里程碑，这里先用固定价。
const unitPriceCents = 1800

// NewOrderMilkTeaTool 构造下单工具。
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
			return &orderMilkTeaResult{
				OrderID: uuid.New().String(),
				Flavor:  args.Flavor,
				Size:    args.Size,
				Sugar:   args.Sugar,
				Count:   count,
				Amount:  count * unitPriceCents,
				ETA:     "15 分钟",
			}, nil
		})
	if err != nil {
		log.Printf("[tools] 构建 %s 工具失败: %v", ToolNameOrderMilkTea, err)
		return nil
	}
	return t
}
