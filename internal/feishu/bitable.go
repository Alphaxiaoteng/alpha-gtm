package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Bitable field definitions. Dates and keys are plain text on purpose: a Base
// datetime cell carries a timezone the CLI would have to reinterpret, while a
// text cell keeps exactly the value the collector recorded.
const (
	PostTableName    = "作品库"
	MetricsTableName = "账号日报"
)

var postTableFields = []map[string]any{
	{"name": "作品键", "type": "text"},
	{"name": "平台", "type": "text"},
	{"name": "账号", "type": "text"},
	{"name": "标题", "type": "text"},
	{"name": "发布时间", "type": "text"},
	{"name": "链接", "type": "url"},
	{"name": "播放", "type": "number"},
	{"name": "点赞", "type": "number"},
	{"name": "评论", "type": "number"},
	{"name": "转发", "type": "number"},
	{"name": "收藏", "type": "number"},
	{"name": "采集日期", "type": "text"},
}

var metricsTableFields = []map[string]any{
	{"name": "账号日期", "type": "text"},
	{"name": "平台", "type": "text"},
	{"name": "账号", "type": "text"},
	{"name": "采集日期", "type": "text"},
	{"name": "统计天数", "type": "number"},
	{"name": "范围已验证", "type": "checkbox"},
	{"name": "粉丝", "type": "number"},
	{"name": "播放", "type": "number"},
	{"name": "点赞", "type": "number"},
	{"name": "评论", "type": "number"},
	{"name": "转发", "type": "number"},
	{"name": "收藏", "type": "number"},
	{"name": "作品数", "type": "number"},
}

// ContentBase is the created Base and its two tables.
type ContentBase struct {
	Token          string `json:"base_token"`
	URL            string `json:"base_url"`
	PostTableID    string `json:"post_table_id"`
	MetricsTableID string `json:"metrics_table_id"`
}

// CreateContentBase creates one Base with the 作品库 table and then adds the
// 账号日报 table, returning the coordinates the app stores.
func (client *Client) CreateContentBase(ctx context.Context, name string) (*ContentBase, error) {
	if strings.TrimSpace(name) == "" {
		name = "Alpha-Nexus 内容库"
	}
	fields, err := json.Marshal(postTableFields)
	if err != nil {
		return nil, err
	}
	payload, err := client.Call(ctx, "base", "+base-create",
		"--name", name,
		"--table-name", PostTableName,
		"--fields", string(fields),
		"--time-zone", "Asia/Shanghai")
	if err != nil {
		return nil, err
	}
	tree, err := decode(payload)
	if err != nil {
		return nil, err
	}
	base := &ContentBase{
		Token:       firstNonEmpty(findString(tree, "base_token"), findString(tree, "app_token")),
		URL:         firstNonEmpty(findString(tree, "url"), findString(tree, "base_url")),
		PostTableID: findString(tree, "table_id"),
	}
	if base.Token == "" {
		return nil, fmt.Errorf("feishu created a base without a token: %s", string(payload))
	}
	if base.PostTableID == "" {
		// Listing tables is the documented fallback when create omits the id.
		base.PostTableID, err = client.firstTableID(ctx, base.Token, PostTableName)
		if err != nil {
			return nil, err
		}
	}
	metricsID, err := client.CreateTable(ctx, base.Token, MetricsTableName, metricsTableFields)
	if err != nil {
		return nil, err
	}
	base.MetricsTableID = metricsID
	if base.URL == "" {
		base.URL = "https://feishu.cn/base/" + base.Token
	}
	return base, nil
}

// CreateTable adds one table with an explicit schema and returns its id.
func (client *Client) CreateTable(ctx context.Context, baseToken, name string, fields []map[string]any) (string, error) {
	if strings.TrimSpace(baseToken) == "" {
		return "", fmt.Errorf("feishu base token is required to create a table")
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	payload, err := client.Call(ctx, "base", "+table-create",
		"--base-token", baseToken, "--name", name, "--fields", string(encoded))
	if err != nil {
		return "", err
	}
	tree, err := decode(payload)
	if err != nil {
		return "", err
	}
	tableID := findString(tree, "table_id")
	if tableID == "" {
		return client.firstTableID(ctx, baseToken, name)
	}
	return tableID, nil
}

// firstTableID lists tables and returns the one with the given name, falling
// back to the single table when the name is not echoed back.
func (client *Client) firstTableID(ctx context.Context, baseToken, name string) (string, error) {
	payload, err := client.Call(ctx, "base", "+table-list", "--base-token", baseToken)
	if err != nil {
		return "", err
	}
	tree, err := decode(payload)
	if err != nil {
		return "", err
	}
	items, _ := tree.(map[string]any)
	var tables []any
	if items != nil {
		for _, key := range []string{"items", "tables"} {
			if list, ok := items[key].([]any); ok {
				tables = list
				break
			}
		}
	}
	var fallback string
	for _, item := range tables {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := record["table_id"].(string)
		if id == "" {
			id, _ = record["id"].(string)
		}
		if id == "" {
			continue
		}
		if fallback == "" {
			fallback = id
		}
		if tableName, _ := record["name"].(string); tableName == name {
			return id, nil
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", fmt.Errorf("feishu base %s has no tables", baseToken)
}

// UpsertRecord writes one record: it updates recordID when known, otherwise it
// creates a record and returns the new id.
func (client *Client) UpsertRecord(ctx context.Context, baseToken, tableID, recordID string, fields map[string]any) (string, error) {
	if strings.TrimSpace(baseToken) == "" || strings.TrimSpace(tableID) == "" {
		return "", fmt.Errorf("feishu base and table are required to write a record")
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	args := []string{"base", "+record-upsert", "--base-token", baseToken, "--table-id", tableID, "--json", string(encoded)}
	if strings.TrimSpace(recordID) != "" {
		args = append(args, "--record-id", recordID)
	}
	payload, err := client.Call(ctx, args...)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(recordID) != "" {
		return recordID, nil
	}
	tree, err := decode(payload)
	if err != nil {
		return "", err
	}
	created := firstNonEmpty(findString(tree, "record_id"), findString(tree, "id"))
	if created == "" {
		return "", fmt.Errorf("feishu accepted a record without returning its id: %s", string(payload))
	}
	return created, nil
}

// Status reports which identity the CLI would use and whether it is usable.
type Status struct {
	Profile     string `json:"profile"`
	AppID       string `json:"app_id"`
	Brand       string `json:"brand"`
	Identity    string `json:"identity"`
	TokenStatus string `json:"token_status"`
	Available   bool   `json:"available"`
	Binary      string `json:"binary"`
	UserStatus  string `json:"user_status"`
}

// Status asks the CLI who it is. `whoami` takes neither --format nor --as, so
// it runs through the raw runner instead of Call.
func (client *Client) Status(ctx context.Context) (*Status, error) {
	if client == nil || client.Runner == nil {
		return nil, fmt.Errorf("feishu client unavailable")
	}
	raw, err := client.Runner.Run(ctx, "whoami")
	if err != nil {
		return nil, err
	}
	payload := json.RawMessage(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, err
	}
	tree, err := decode(payload)
	if err != nil {
		return nil, err
	}
	status := &Status{Binary: ResolveBinary()}
	if node, ok := tree.(map[string]any); ok {
		status.Profile, _ = node["profile"].(string)
		status.AppID, _ = node["appId"].(string)
		status.Brand, _ = node["brand"].(string)
		status.Identity, _ = node["identity"].(string)
		status.TokenStatus, _ = node["tokenStatus"].(string)
		status.Available, _ = node["available"].(bool)
	}
	return status, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
