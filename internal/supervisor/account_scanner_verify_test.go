package supervisor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// 验证打假后的行为 —— 用**临时夹具**而不是本机真实缓存：
//
//  1. 命中账号时必须读到 Available=true，且模型清单来自它自己的缓存描述（非写死）；
//  2. 不存在的账号必须 Available=false 且模型清单为空 —— 绝不编造。
//
// 为什么改成夹具：原实现直接查本机的真实授权缓存，那一来依赖第三方工具在跑、
// 二来把某个真实邮箱写进了测试源码。这条测试要验的是**解析逻辑**，
// 与「本机此刻有没有那个账号的缓存」无关。
func TestQueryCacheForHonesty(t *testing.T) {
	dir := t.TempDir()
	authorized := filepath.Join(dir, "cache", "quota_api_v1_desktop", "authorized")
	if err := os.MkdirAll(authorized, 0o755); err != nil {
		t.Fatalf("创建夹具目录失败: %v", err)
	}
	t.Setenv("COCKPIT_TOOLS_DATA_DIR", dir)

	// 夹具：一个带双池与模型描述的账号。
	const known = "fixture-account@example.com"
	payload := map[string]any{
		"email": known,
		"payload": map[string]any{
			"quota_summary": map[string]any{
				"groups": []map[string]any{
					{
						"displayName": "Gemini Models",
						"description": "Models within this group: Fixture Flash, Fixture Pro",
						"buckets": []map[string]any{
							{"window": "5h", "remainingFraction": 0.99},
							{"window": "weekly", "remainingFraction": 0.82},
						},
					},
					{
						"displayName": "Claude & GPT Models",
						"description": "Models within this group: Fixture Opus",
						"buckets": []map[string]any{
							{"window": "5h", "remainingFraction": 1.0},
							{"window": "weekly", "remainingFraction": 1.0},
						},
					},
				},
			},
		},
	}
	raw, _ := json.Marshal(payload)
	if err := os.WriteFile(filepath.Join(authorized, "fixture.json"), raw, 0o644); err != nil {
		t.Fatalf("写夹具失败: %v", err)
	}

	g, c, models, ok := queryCacheFor(known)
	if !ok {
		t.Fatal("夹具账号应被命中，实际未命中")
	}
	if !g.Available || !c.Available {
		t.Errorf("双池都应标记为可用: gemini=%v claude=%v", g.Available, c.Available)
	}
	if g.FiveHourPercent != 99 || g.WeeklyPercent != 82 {
		t.Errorf("Gemini 池读数应来自夹具（5h=99 wk=82），实际 5h=%d wk=%d", g.FiveHourPercent, g.WeeklyPercent)
	}
	if len(models) == 0 {
		t.Error("模型清单应来自缓存里的 Models within this group 描述，不应为空")
	}

	g2, c2, m2, ok2 := queryCacheFor("definitely-not-a-real-account@example.invalid")
	if ok2 || g2.Available || c2.Available || len(m2) != 0 {
		t.Errorf("未知账号必须如实返回空数据: ok=%v g=%v c=%v models=%v", ok2, g2.Available, c2.Available, m2)
	}
}
