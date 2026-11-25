package llm

import "testing"

func TestUsageAdd(t *testing.T) {
	total := Usage{InputTokens: 10, OutputTokens: 3}
	total.Add(Usage{InputTokens: 5, OutputTokens: 4, CacheWriteTokens: 7})
	want := Usage{InputTokens: 15, OutputTokens: 7, CacheWriteTokens: 7}
	if total != want {
		t.Fatalf("Add = %+v, want = %+v", total, want)
	}
}

func TestUsageMergeEcho(t *testing.T) {
	u := Usage{InputTokens: 500, CacheReadTokens: 200}
	u.MergeEcho(Usage{InputTokens: 600, CacheReadTokens: 200, OutputTokens: 300})
	if u.InputTokens != 600 || u.CacheReadTokens != 200 || u.OutputTokens != 300 {
		t.Fatalf("MergeEcho = %+v", u)
	}
	v := Usage{}
	v.MergeEcho(Usage{InputTokens: 510, OutputTokens: 50})
	if v.InputTokens != 510 {
		t.Fatalf("InputTokens = %d, want 510", v.InputTokens)
	}
}

func TestUsageMergeEchoPlaceholderOutput(t *testing.T) {
	u := Usage{InputTokens: 500, OutputTokens: 1} // 占位 output
	u.MergeEcho(Usage{InputTokens: 510, OutputTokens: 99})
	if u.OutputTokens != 99 {
		t.Fatalf("占位 output 应被累计值覆盖: OutputTokens = %d, want 99", u.OutputTokens)
	}
}

func TestUsageMergeEchoNoShrinkNoWipe(t *testing.T) {
	u := Usage{InputTokens: 600, OutputTokens: 99, CacheWriteTokens: 40}
	u.MergeEcho(Usage{InputTokens: 500})
	if u.InputTokens != 600 {
		t.Fatalf("回声变小不应回退: InputTokens = %d, want 600", u.InputTokens)
	}
	u.MergeEcho(Usage{})
	if u.OutputTokens != 99 || u.CacheWriteTokens != 40 {
		t.Fatalf("空载事件不应抹值: %+v", u)
	}
	u.MergeEcho(Usage{CacheWriteTokens: 70})
	if u.CacheWriteTokens != 70 {
		t.Fatalf("CacheWrite max = %d, want 70", u.CacheWriteTokens)
	}
}
