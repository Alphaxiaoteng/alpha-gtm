package db

import "testing"

func TestTikHubAPICallLifecyclePersistsDetailedRecord(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	call, err := database.CreateTikHubAPICall(TikHubAPICallInput{
		TraceID:        "trace-1",
		PlatformCode:   "douyin",
		EndpointID:     "douyin.hot_search",
		Capability:     "daily_rank",
		Method:         "GET",
		Path:           "/api/v1/douyin/app/v3/fetch_hot_search_list",
		ParametersJSON: `{"board_type":"0"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if call.Status != "running" || call.ID == 0 {
		t.Fatalf("unexpected created call: %#v", call)
	}
	finished, err := database.FinishTikHubAPICall(call.ID, TikHubAPICallFinish{
		Status:        "succeeded",
		ProviderCode:  "200",
		ItemCount:     50,
		ResponseBytes: 4096,
		RawPath:       "/tmp/tikhub/calls/record.json",
		DurationMS:    321,
	})
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != "succeeded" || finished.ItemCount != 50 || finished.RawPath == "" || finished.FinishedAt == "" {
		t.Fatalf("unexpected finished call: %#v", finished)
	}
	calls, err := database.ListTikHubAPICalls(TikHubAPICallFilter{PlatformCode: "douyin", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].ID != call.ID || calls[0].ParametersJSON == "" {
		t.Fatalf("unexpected calls: %#v", calls)
	}
}

func TestRecoverRunningTikHubAPICallsMarksInterruptedWork(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	call, err := database.CreateTikHubAPICall(TikHubAPICallInput{
		TraceID: "trace-interrupted", PlatformCode: "douyin", EndpointID: "douyin.video_search",
		Capability: "keyword_search", Method: "GET", Path: "/api/v1/douyin/search/fetch_video_search_result",
		ParametersJSON: `{"keyword":"AI"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := database.RecoverRunningTikHubAPICalls()
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("recovered=%d", recovered)
	}
	got, err := database.GetTikHubAPICall(call.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "failed" || got.ErrorCode != "local_process_interrupted" || got.FinishedAt == "" {
		t.Fatalf("unexpected recovered call: %#v", got)
	}
}
