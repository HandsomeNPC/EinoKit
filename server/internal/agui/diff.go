package agui

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
)

// DiffState 计算 before -> after 的 JSON Patch(RFC 6902)操作序列。
// before/after 可以是任意可被 json.Marshal 的值(结构体、map、切片、标量)。
//
// 规则:
//   - 两边都是 object(map):按字段递归 diff,产出 add/remove/replace。
//   - 一边是 object 另一边不是、或都是数组/标量且不等:整体 replace。
//   - 字段从无到有:add;从有到无:remove。
//
// 注意:数组按整体比较,不做逐元素 patch(对本项目的 state 快照足够)。
func DiffState(before, after any) []events.JSONPatchOperation {
	b := normalizeJSON(before)
	a := normalizeJSON(after)
	var ops []events.JSONPatchOperation
	diffValue("", b, a, &ops)
	return ops
}

// normalizeJSON 先 marshal 再 unmarshal,让结构体的 json tag 生效,
// 并把所有数字统一成 float64、统一 map/slice/scalar 类型,便于后续比较。
func normalizeJSON(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return v
	}
	return out
}

func diffValue(path string, before, after any, ops *[]events.JSONPatchOperation) {
	switch {
	case before == nil && after == nil:
		return
	case before == nil:
		*ops = append(*ops, events.JSONPatchOperation{Op: "add", Path: path, Value: after})
		return
	case after == nil:
		*ops = append(*ops, events.JSONPatchOperation{Op: "remove", Path: path})
		return
	}

	bm, bok := before.(map[string]any)
	am, aok := after.(map[string]any)
	if bok && aok {
		diffMap(path, bm, am, ops)
		return
	}

	// 类型不同或都是数组/标量:整体比较,不等则 replace。
	if !reflect.DeepEqual(before, after) {
		*ops = append(*ops, events.JSONPatchOperation{Op: "replace", Path: path, Value: after})
	}
}

func diffMap(path string, before, after map[string]any, ops *[]events.JSONPatchOperation) {
	keys := make(map[string]struct{}, len(before)+len(after))
	for k := range before {
		keys[k] = struct{}{}
	}
	for k := range after {
		keys[k] = struct{}{}
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	for _, k := range sorted {
		child := joinPointer(path, k)
		bv, bok := before[k]
		av, aok := after[k]
		switch {
		case !bok && aok: // 新增字段
			*ops = append(*ops, events.JSONPatchOperation{Op: "add", Path: child, Value: av})
		case bok && !aok: // 字段移除
			*ops = append(*ops, events.JSONPatchOperation{Op: "remove", Path: child})
		default:
			diffValue(child, bv, av, ops)
		}
	}
}

// joinPointer 拼接 JSON Pointer 路径,按 RFC 6902 转义:先 ~ -> ~0,再 / -> ~1。
func joinPointer(base, key string) string {
	escaped := strings.ReplaceAll(key, "~", "~0")
	escaped = strings.ReplaceAll(escaped, "/", "~1")
	return base + "/" + escaped
}
