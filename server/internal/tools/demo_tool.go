package tools

import (
	"context"
	"encoding/gob"
	"fmt"
	"log"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// ToolNameDemo 是下单工具名。
const ToolNameDemo = "Demo"

// demoArgs 是下单工具的入参。
type demoArgs struct {
	Flavor string `json:"flavor" jsonschema:"description=奶茶口味，如 芋泥波波 / 四季春"`
	Size   string `json:"size" jsonschema:"description=杯型,enum=中杯,enum=大杯"`
	Sugar  string `json:"sugar" jsonschema:"description=甜度,enum=无糖,enum=三分糖,enum=五分糖,enum=全糖"`
	Count  int    `json:"count" jsonschema:"description=杯数，至少 1"`
}

// demoResult 是下单工具的返回值。
type demoResult struct {
	Cancelled bool   `json:"cancelled,omitempty"`
	Message   string `json:"message,omitempty"`
	OrderID   string `json:"orderId,omitempty"`
}

type demoInfo struct {
	Message string `json:"message,omitempty"`
}

// unitPriceCents 单杯价格（分）。订单落库是后续里程碑，这里先用固定价。
const unitPriceCents = 1800

func init() {
	gob.Register(&demoArgs{})
}

// NewDemoTool 构造下单工具。
func NewDemoTool() tool.InvokableTool {
	t, err := utils.InferTool(ToolNameDemo, "下单购买奶茶。", demoToolInvoke)
	if err != nil {
		log.Printf("[tools] 构建 %s 工具失败: %v", ToolNameDemo, err)
		return nil
	}
	return t
}

// demoToolInvoke 是下单工具的业务实现。
// 首次调用时中断等待用户确认，并把入参存入 interrupt state；恢复时根据用户决定落单或取消。
func demoToolInvoke(ctx context.Context, args *demoArgs) (*demoResult, error) {
	if args == nil {
		return nil, fmt.Errorf("下单参数为空")
	}
	wasInterrupted, _, savedArgs := tool.GetInterruptState[*demoArgs](ctx)

	if !wasInterrupted {
		return nil, tool.StatefulInterrupt(ctx, buildDemoInfo(args).Message, args)
	}
	isTarget, hasData, resumeEntry := tool.GetResumeContext[types.ResumeEntry](ctx)
	if isTarget && hasData {
		// 用户取消：如实返回未创建订单，让模型据此回复，而不是照样下单。
		if resumeEntry.Status == types.ResumeStatusCancelled {
			return &demoResult{Cancelled: true, Message: "用户取消了本次下单，订单未创建。"}, nil
		}
		log.Printf("[tools] %s 收到用户确认: %v", ToolNameDemo, resumeEntry.Payload)
		return &demoResult{Cancelled: false, Message: "下单成功", OrderID: "1234567890"}, nil
	}

	isTarget, _, _ = tool.GetResumeContext[any](ctx)
	if !isTarget {
		return nil, tool.StatefulInterrupt(ctx, buildDemoInfo(args).Message, savedArgs)
	}
	return nil, nil
}

// buildDemoInfo 拼装下单确认提示。count<=0 时按 1 杯计。
func buildDemoInfo(args *demoArgs) *demoInfo {
	count := args.Count
	if count <= 0 {
		count = 1
	}
	return &demoInfo{
		Message: fmt.Sprintf("确认下单 %d 杯%s（%s / %s），合计 %.2f 元？",
			count, args.Flavor, args.Size, args.Sugar,
			float64(count*unitPriceCents)/100),
	}
}
