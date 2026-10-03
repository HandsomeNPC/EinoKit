package loop

import (
	"bytes"
	"encoding/gob"
	"testing"
)

// TestGobRegistration 验证三个类型都能以**接口值**的形式过 gob。
//
// 为什么值得单测：checkpoint 用 gob 编码，而 ApprovalResult 挂在
// adk.ResumeParams.Targets（map[string]any）里、ApprovalInfo 挂在
// adk.InterruptCtx.Info（any）里。漏注册的话编码会报
// "gob: type not registered for interface"，而且只在**恢复**时才炸——
// 首轮对话一切正常，极难排查。这个测试把它挪到编译期之后、跑服务之前。
func TestGobRegistration(t *testing.T) {
	reason := "太贵了"
	cases := []struct {
		name string
		val  any
	}{
		{"ChatItem", &ChatItem{Query: "来杯奶茶"}},
		{"ChatItem_withApproval", &ChatItem{
			ApprovalResult: &ApprovalResult{Approved: false, DisapproveReason: &reason},
			InterruptID:    "int-1",
		}},
		{"ApprovalResult", &ApprovalResult{Approved: true}},
		{"ApprovalInfo", &ApprovalInfo{ToolName: "order_milk_tea", ArgumentsInJSON: `{"count":1}`}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 存进 any 再编码，复现框架把它们放进接口字段的实际情形。
			var buf bytes.Buffer
			payload := map[string]any{"v": tc.val}
			if err := gob.NewEncoder(&buf).Encode(payload); err != nil {
				t.Fatalf("gob 编码失败(很可能漏了 schema.RegisterName): %v", err)
			}

			var out map[string]any
			if err := gob.NewDecoder(&buf).Decode(&out); err != nil {
				t.Fatalf("gob 解码失败: %v", err)
			}
			if out["v"] == nil {
				t.Fatal("解码后值为 nil")
			}
		})
	}
}

// TestApprovalResultRoundTrip 确认审批结论过了 gob 之后仍能被正确取用。
//
// 注意 gob 会把接口值里的指针摊平：编码 *ApprovalResult，解码出来是
// ApprovalResult。所以中间件必须经 ResumedApproval 归一化，而不是直接断言指针。
func TestApprovalResultRoundTrip(t *testing.T) {
	reason := "改主意了"
	var buf bytes.Buffer
	in := map[string]any{"t": &ApprovalResult{Approved: false, DisapproveReason: &reason}}
	if err := gob.NewEncoder(&buf).Encode(in); err != nil {
		t.Fatalf("编码失败: %v", err)
	}

	var out map[string]any
	if err := gob.NewDecoder(&buf).Decode(&out); err != nil {
		t.Fatalf("解码失败: %v", err)
	}

	got, ok := ResumedApproval(out["t"])
	if !ok {
		t.Fatalf("ResumedApproval 未能识别 %T", out["t"])
	}
	if got.Approved {
		t.Error("Approved 应为 false")
	}
	if got.DisapproveReason == nil || *got.DisapproveReason != reason {
		t.Errorf("DisapproveReason 丢失或不匹配: %v", got.DisapproveReason)
	}
}

// TestResumedApproval 覆盖归一化函数的三种输入。
func TestResumedApproval(t *testing.T) {
	if got, ok := ResumedApproval(&ApprovalResult{Approved: true}); !ok || !got.Approved {
		t.Error("指针形式应被识别为已批准")
	}
	if got, ok := ResumedApproval(ApprovalResult{Approved: true}); !ok || !got.Approved {
		t.Error("值形式应被识别为已批准")
	}
	if _, ok := ResumedApproval("不是审批结论"); ok {
		t.Error("无关类型不应被识别")
	}
	if _, ok := ResumedApproval((*ApprovalResult)(nil)); ok {
		t.Error("nil 指针不应被识别")
	}
}

// TestApprovalInfoSatisfiesBridge 确认值与指针两种形式都满足桥接层的取值接口。
//
// 这一点容易踩坑：方法若写成指针接收者，gob 摊平后的值形式就不满足接口，
// 审批卡片会丢掉工具名与参数（但不报错，只是卡片内容为空）。
func TestApprovalInfoSatisfiesBridge(t *testing.T) {
	type bridgeView interface {
		GetToolName() string
		GetArgumentsInJSON() string
	}

	info := ApprovalInfo{ToolName: "order_milk_tea", ArgumentsInJSON: `{"count":2}`}
	for name, v := range map[string]any{"值": info, "指针": &info} {
		got, ok := v.(bridgeView)
		if !ok {
			t.Fatalf("%s形式不满足桥接层接口（方法接收者是否写成了指针？）", name)
		}
		if got.GetToolName() != "order_milk_tea" || got.GetArgumentsInJSON() != `{"count":2}` {
			t.Errorf("%s形式取值不正确", name)
		}
	}
}
