package agents

import (
	"testing"
)

func TestShouldEnableReasoning(t *testing.T) {
	// 1. Auto mode - Short messages should return false
	if ShouldEnableReasoning("1", "auto") != false {
		t.Fatal("expected false for '1'")
	}
	if ShouldEnableReasoning("hi", "auto") != false {
		t.Fatal("expected false for 'hi'")
	}
	if ShouldEnableReasoning("好的，收到", "auto") != false {
		t.Fatal("expected false for short response")
	}

	// 2. Auto mode - Complex keywords or code should return true
	if ShouldEnableReasoning("帮我修复这段代码的 error 报错问题", "auto") != true {
		t.Fatal("expected true for code error query")
	}
	if ShouldEnableReasoning("请解释一下这个并发架构的设计原理", "auto") != true {
		t.Fatal("expected true for architecture query")
	}

	// 3. None mode - Always false
	if ShouldEnableReasoning("请解释一下这个并发架构的设计原理", "none") != false {
		t.Fatal("expected false in none mode")
	}

	// 4. Deep mode - Always true
	if ShouldEnableReasoning("1", "deep") != true {
		t.Fatal("expected true in deep mode")
	}
}
