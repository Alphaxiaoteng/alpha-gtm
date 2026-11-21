package agenthub

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stressLongTaskRunner simulates a multi-step long-running agent task that
// streams many message deltas and observed tool calls concurrently across bots.
type stressLongTaskRunner struct {
	chunksPerRun int
	toolsPerRun  int
	chunkDelay   time.Duration
	lastRequests sync.Map // conversationID -> RunRequest
	totalEvents  atomic.Int64
}

func (r *stressLongTaskRunner) Run(ctx context.Context, request RunRequest, emit func(ProviderEvent)) (string, error) {
	r.lastRequests.Store(request.ConversationID, request)
	for step := 0; step < r.toolsPerRun; step++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		emit(ProviderEvent{
			Type: "tool.observed",
			Payload: map[string]any{
				"call_id":   fmt.Sprintf("call_%s_%d", request.RunID, step),
				"tool_name": fmt.Sprintf("stress_tool_%d", step),
				"status":    "completed",
			},
		})
		r.totalEvents.Add(1)
	}

	for i := 0; i < r.chunksPerRun; i++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		if r.chunkDelay > 0 {
			time.Sleep(r.chunkDelay)
		}
		text := fmt.Sprintf("段落-%d ", i+1)
		if i == r.chunksPerRun-1 {
			text += fmt.Sprintf("以后请记住：本轮任务 %s 的核心参数与报价口径已核验完成。风险教训：发货前必须复核孔数。", request.RunID)
		}
		emit(ProviderEvent{
			Type:    "message.delta",
			Payload: map[string]any{"text": text},
		})
		r.totalEvents.Add(1)
	}
	// Return empty session ID to also exercise stateless multi-turn conversation memory injection.
	return "", nil
}

func TestFiveGuanyingBotsPresetAndFourSectionMemory(t *testing.T) {
	service := setupMemoryService(t)
	ctx := context.Background()

	if err := service.EnsureGuanyingHackathonPreset(ctx); err != nil {
		t.Fatalf("EnsureGuanyingHackathonPreset failed: %v", err)
	}

	expectedSpecialists := []struct {
		id     string
		name   string
		avatar string
	}{
		{"bot_gy_content_ops", "内容运营 Bot", "/bot-avatars/writer.svg"},
		{"bot_gy_seo", "SEO Bot", "/bot-avatars/planner.svg"},
		{"bot_gy_tech", "技术 Bot", "/bot-avatars/engineer.svg"},
		{"bot_gy_data", "数据分析 Bot", "/bot-avatars/analyst.svg"},
		{"bot_gy_sales", "外贸跟单 Bot", "/bot-avatars/support.svg"},
	}

	bots, err := service.ListBots(ctx)
	if err != nil {
		t.Fatalf("ListBots failed: %v", err)
	}
	byID := make(map[string]Bot, len(bots))
	for _, b := range bots {
		byID[b.ID] = b
	}

	for _, exp := range expectedSpecialists {
		bot, ok := byID[exp.id]
		if !ok {
			t.Fatalf("missing specialist bot %s (%s)", exp.id, exp.name)
		}
		if bot.Name != exp.name {
			t.Fatalf("bot %s name = %q, want %q", exp.id, bot.Name, exp.name)
		}
		if bot.Avatar != exp.avatar {
			t.Fatalf("bot %s avatar = %q, want %q", exp.id, bot.Avatar, exp.avatar)
		}
		mem, err := service.ReadBotMemory(ctx, exp.id)
		if err != nil {
			t.Fatalf("ReadBotMemory(%s) failed: %v", exp.id, err)
		}
		for _, sec := range botMemorySections {
			if !strings.Contains(mem, "## "+sec) {
				t.Fatalf("bot %s memory missing section %q:\n%s", exp.id, sec, mem)
			}
		}
		if strings.Contains(mem, botMemoryEmptyEntry) {
			t.Fatalf("bot %s should have all 4 memory sections populated without placeholders:\n%s", exp.id, mem)
		}
	}
}

func TestFiveBotsConcurrentLongTaskAndMemoryStress(t *testing.T) {
	runner := &stressLongTaskRunner{
		chunksPerRun: 35,
		toolsPerRun:  5,
		chunkDelay:   1 * time.Millisecond,
	}
	service := setupService(t, runner)
	ctx := context.Background()

	if err := service.EnsureGuanyingHackathonPreset(ctx); err != nil {
		t.Fatalf("EnsureGuanyingHackathonPreset failed: %v", err)
	}

	botIDs := []string{
		"bot_gy_content_ops",
		"bot_gy_seo",
		"bot_gy_tech",
		"bot_gy_data",
		"bot_gy_sales",
	}
	const turnsPerBot = 4

	type runMetric struct {
		botID      string
		turn       int
		durationMS float64
	}
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		metrics  []runMetric
		failures []string
	)

	startedAt := time.Now()
	for botIdx, botID := range botIDs {
		wg.Add(1)
		go func(bIdx int, bID string) {
			defer wg.Done()
			conv, err := service.CreateConversation(ctx, ProviderCodex, "压测会话-"+bID, testVerifiedModel, ContextSnapshot{})
			if err != nil {
				mu.Lock()
				failures = append(failures, fmt.Sprintf("%s create conv: %v", bID, err))
				mu.Unlock()
				return
			}
			if _, err := service.SetConversationBot(ctx, conv.ID, bID); err != nil {
				mu.Lock()
				failures = append(failures, fmt.Sprintf("%s bind bot: %v", bID, err))
				mu.Unlock()
				return
			}

			for turn := 1; turn <= turnsPerBot; turn++ {
				turnStart := time.Now()
				userMsg := fmt.Sprintf(
					"第%d轮长任务压测。用户偏好：统一输出表格-%s-T%d。品牌语气：保持硬核工业调性-%s-T%d。",
					turn, bID, turn, bID, turn,
				)
				run, err := service.StartRun(ctx, conv.ID, ProviderCodex, testVerifiedModel, userMsg, ContextSnapshot{})
				if err != nil {
					mu.Lock()
					failures = append(failures, fmt.Sprintf("%s turn %d StartRun: %v", bID, turn, err))
					mu.Unlock()
					return
				}

				// Concurrently read & append bot memory while the long task is streaming
				_ = service.AppendBotMemory(ctx, bID, "关键事实", fmt.Sprintf("并发写入事实锚点-%s-T%d", bID, turn))
				_, _ = service.ReadBotMemory(ctx, bID)

				deadline := time.Now().Add(45 * time.Second)
				completed := false
				for time.Now().Before(deadline) {
					current, getErr := service.GetRun(ctx, run.ID)
					if getErr == nil && current.Status == "completed" {
						completed = true
						break
					}
					if getErr == nil && (current.Status == "failed" || current.Status == "cancelled") {
						mu.Lock()
						failures = append(failures, fmt.Sprintf("%s turn %d status=%s err=%s", bID, turn, current.Status, current.ErrorCode))
						mu.Unlock()
						return
					}
					time.Sleep(5 * time.Millisecond)
				}
				if !completed {
					mu.Lock()
					failures = append(failures, fmt.Sprintf("%s turn %d timed out waiting for completion", bID, turn))
					mu.Unlock()
					return
				}

				mu.Lock()
				metrics = append(metrics, runMetric{
					botID:      bID,
					turn:       turn,
					durationMS: float64(time.Since(turnStart).Microseconds()) / 1000.0,
				})
				mu.Unlock()
			}

			// Verify multi-turn conversation memory block was injected by the last turn
			if rawReq, ok := runner.lastRequests.Load(conv.ID); ok {
				req := rawReq.(RunRequest)
				if !strings.Contains(req.Message, "【Bot 记忆】") {
					mu.Lock()
					failures = append(failures, fmt.Sprintf("%s prompt missing 【Bot 记忆】", bID))
					mu.Unlock()
				}
				if !strings.Contains(req.Message, "【近期对话上下文记忆】") {
					mu.Lock()
					failures = append(failures, fmt.Sprintf("%s prompt missing 【近期对话上下文记忆】 on multi-turn run", bID))
					mu.Unlock()
				}
			}
		}(botIdx, botID)
	}

	wg.Wait()
	totalElapsed := time.Since(startedAt)

	if len(failures) > 0 {
		t.Fatalf("stress test had %d failures:\n%s", len(failures), strings.Join(failures, "\n"))
	}
	if len(metrics) != len(botIDs)*turnsPerBot {
		t.Fatalf("completed runs = %d, want %d", len(metrics), len(botIDs)*turnsPerBot)
	}

	// Verify every bot's memory file persisted all user preferences, tones, lessons, and concurrent facts without corruption
	for _, bID := range botIDs {
		mem, err := service.ReadBotMemory(ctx, bID)
		if err != nil {
			t.Fatalf("ReadBotMemory(%s) after stress: %v", bID, err)
		}
		for turn := 1; turn <= turnsPerBot; turn++ {
			wantPref := fmt.Sprintf("用户偏好：统一输出表格-%s-T%d。", bID, turn)
			wantTone := fmt.Sprintf("品牌语气：保持硬核工业调性-%s-T%d。", bID, turn)
			wantFact := fmt.Sprintf("并发写入事实锚点-%s-T%d", bID, turn)
			for _, token := range []string{wantPref, wantTone, wantFact, "风险教训：发货前必须复核孔数。"} {
				if !strings.Contains(mem, token) {
					t.Fatalf("bot %s memory lost token %q under concurrency:\n%s", bID, token, mem)
				}
			}
		}
	}

	durations := make([]float64, 0, len(metrics))
	for _, m := range metrics {
		durations = append(durations, m.durationMS)
	}
	sort.Float64s(durations)
	p50 := durations[len(durations)/2]
	p95 := durations[(len(durations)*95)/100]
	maxDur := durations[len(durations)-1]
	totalEv := runner.totalEvents.Load()
	evPerSec := float64(totalEv) / totalElapsed.Seconds()

	t.Logf(
		"STRESS SUMMARY: bots=%d turns=%d total_runs=%d success_rate=100%% total_events=%d elapsed=%s throughput=%.1f events/s latency_p50=%.1fms latency_p95=%.1fms latency_max=%.1fms",
		len(botIDs), turnsPerBot, len(metrics), totalEv, totalElapsed.Round(time.Millisecond), evPerSec, p50, p95, maxDur,
	)
}

type panicLongTaskRunner struct{}

func (panicLongTaskRunner) Run(_ context.Context, _ RunRequest, emit func(ProviderEvent)) (string, error) {
	emit(ProviderEvent{Type: "message.delta", Payload: map[string]any{"text": "半截输出"}})
	panic("simulated long task provider crash")
}

func TestLongTaskPanicRecoveryFailsCleanlyWithoutZombieRun(t *testing.T) {
	service := setupService(t, panicLongTaskRunner{})
	ctx := context.Background()

	conv, err := service.CreateConversation(ctx, ProviderCodex, "panic恢复测试", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.StartRun(ctx, conv.ID, ProviderCodex, testVerifiedModel, "触发异常长任务", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, service, run.ID, "failed")

	finished, err := service.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.ErrorCode != "AGENT_PROVIDER_PANIC" {
		t.Fatalf("error code = %q, want AGENT_PROVIDER_PANIC", finished.ErrorCode)
	}

	// Ensure conversation is not locked by a zombie run and can start a follow-up run
	run2, err := service.StartRun(ctx, conv.ID, ProviderCodex, testVerifiedModel, "灾后重试", ContextSnapshot{})
	if err != nil {
		t.Fatalf("follow-up StartRun after panic failed: %v", err)
	}
	waitForRun(t, service, run2.ID, "failed")
}
