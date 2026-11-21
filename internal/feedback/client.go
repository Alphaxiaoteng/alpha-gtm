package feedback

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	defaultFeishuBaseURL = "https://open.feishu.cn"
	maxResponseBytes     = 1 << 20
	maxStatusBatch       = 100
)

var (
	ErrRemoteUnavailable = errors.New("feedback remote unavailable")
	ErrSchemaMismatch    = errors.New("feedback table schema mismatch")
	ErrInvalidInput      = errors.New("feedback input is invalid")
)

type SubmitRequest struct {
	Content         string `json:"content"`
	AppVersion      string `json:"app_version"`
	CurrentModule   string `json:"current_module"`
	OperatingSystem string `json:"operating_system"`
	Channel         string `json:"channel"`
	ClientToken     string `json:"-"`
	FeedbackNumber  string `json:"-"`
	ReadToken       string `json:"-"`
}

type Receipt struct {
	FeedbackID string `json:"feedback_id"`
	ReadToken  string `json:"read_token"`
	Status     string `json:"status"`
	Category   string `json:"category"`
	Reply      string `json:"developer_reply"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

type Credential struct {
	FeedbackID string `json:"feedback_id"`
	ReadToken  string `json:"read_token"`
}

type Status struct {
	FeedbackID string `json:"feedback_id"`
	Status     string `json:"status"`
	Category   string `json:"category"`
	Reply      string `json:"developer_reply"`
	UpdatedAt  string `json:"updated_at"`
}

type Client interface {
	Configured() (bool, error)
	Submit(context.Context, SubmitRequest) (Receipt, error)
	Status(context.Context, []Credential) ([]Status, error)
}

// FeishuClient talks directly to Feishu from the local Go sidecar. Credentials
// are loaded from the macOS Keychain for each operation; tokens remain in memory.
type FeishuClient struct {
	store   ConfigStore
	client  *http.Client
	baseURL string
	now     func() time.Time

	mu            sync.Mutex
	token         string
	tokenExpires  time.Time
	tokenConfigID string
	baseToken     string
	baseConfigID  string
	fieldNames    map[string]bool
	fieldConfigID string
}

func NewFeishuClient(store ConfigStore, timeout time.Duration) *FeishuClient {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &FeishuClient{
		store:   store,
		client:  &http.Client{Timeout: timeout},
		baseURL: defaultFeishuBaseURL,
		now:     time.Now,
	}
}

// Configured reports only whether the local Keychain bundle exists. It never
// loads or returns credential contents.
func (c *FeishuClient) Configured() (bool, error) {
	if c == nil || c.store == nil {
		return false, nil
	}
	return c.store.Configured()
}

// PrepareSubmitRequest generates the identifiers that must survive retries.
// Callers that persist these values before Submit can safely reuse Feishu's
// client_token after a local receipt write fails.
func PrepareSubmitRequest(request SubmitRequest) (SubmitRequest, error) {
	request.Content = strings.TrimSpace(request.Content)
	if count := utf8.RuneCountInString(request.Content); count < 2 || count > 2000 {
		return SubmitRequest{}, ErrInvalidInput
	}
	var err error
	if request.FeedbackNumber = strings.TrimSpace(request.FeedbackNumber); request.FeedbackNumber == "" {
		request.FeedbackNumber, err = randomID("ANX-", 10)
		if err != nil {
			return SubmitRequest{}, fmt.Errorf("generate feedback id: %w", err)
		}
	}
	if request.ClientToken = strings.TrimSpace(request.ClientToken); request.ClientToken == "" {
		request.ClientToken, err = randomUUID()
		if err != nil {
			return SubmitRequest{}, fmt.Errorf("generate feedback idempotency key: %w", err)
		}
	}
	if request.ReadToken = strings.TrimSpace(request.ReadToken); request.ReadToken == "" {
		request.ReadToken, err = randomID("", 32)
		if err != nil {
			return SubmitRequest{}, fmt.Errorf("generate feedback credential: %w", err)
		}
	}
	if len(request.ClientToken) > 64 || len(request.FeedbackNumber) > 64 || len(request.ReadToken) > 128 {
		return SubmitRequest{}, ErrInvalidInput
	}
	return request, nil
}

func newFeishuClientForTest(store ConfigStore, baseURL string, client *http.Client) *FeishuClient {
	return &FeishuClient{
		store:   store,
		client:  client,
		baseURL: strings.TrimRight(baseURL, "/"),
		now:     time.Now,
	}
}

func (c *FeishuClient) Submit(ctx context.Context, request SubmitRequest) (Receipt, error) {
	var err error
	request, err = PrepareSubmitRequest(request)
	if err != nil {
		return Receipt{}, err
	}
	cfg, err := c.loadConfig()
	if err != nil {
		return Receipt{}, err
	}
	token, err := c.tenantToken(ctx, cfg)
	if err != nil {
		return Receipt{}, err
	}
	baseToken, err := c.resolveBaseToken(ctx, cfg, token)
	if err != nil {
		return Receipt{}, err
	}
	fields, err := c.fields(ctx, cfg, baseToken, token)
	if err != nil {
		return Receipt{}, err
	}
	if !fields["需求意见"] {
		return Receipt{}, fmt.Errorf("%w: required field is missing", ErrSchemaMismatch)
	}

	values := map[string]any{"需求意见": request.Content}
	setIfFieldExists(values, fields, "反馈编号", request.FeedbackNumber)
	setIfFieldExists(values, fields, "查询凭证摘要", tokenDigest(request.ReadToken))
	setIfFieldExists(values, fields, "反馈类型", "未分类")
	setIfFieldExists(values, fields, "处理状态", "待处理")
	setIfFieldExists(values, fields, "应用版本", request.AppVersion)
	setIfFieldExists(values, fields, "当前模块", request.CurrentModule)
	setIfFieldExists(values, fields, "操作系统", request.OperatingSystem)
	setIfFieldExists(values, fields, "发布渠道", request.Channel)

	var response struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Record struct {
				RecordID   string         `json:"record_id"`
				Fields     map[string]any `json:"fields"`
				CreatedAt  string         `json:"created_time"`
				UpdatedAt  string         `json:"last_modified_time"`
				UpdatedAt2 string         `json:"updated_time"`
			} `json:"record"`
		} `json:"data"`
	}
	path := fmt.Sprintf(
		"/open-apis/bitable/v1/apps/%s/tables/%s/records?client_token=%s",
		url.PathEscape(baseToken),
		url.PathEscape(cfg.TableID),
		url.QueryEscape(request.ClientToken),
	)
	if err := c.doJSON(ctx, http.MethodPost, path, token, map[string]any{"fields": values}, &response); err != nil {
		return Receipt{}, err
	}
	if response.Code != 0 || strings.TrimSpace(response.Data.Record.RecordID) == "" {
		return Receipt{}, feishuCodeError(response.Code)
	}

	now := c.now().UTC().Format(time.RFC3339)
	createdAt := feishuTime(response.Data.Record.CreatedAt, now)
	updatedAt := feishuTime(firstNonEmpty(response.Data.Record.UpdatedAt, response.Data.Record.UpdatedAt2), createdAt)
	return Receipt{
		FeedbackID: response.Data.Record.RecordID,
		ReadToken:  request.ReadToken,
		Status:     "待处理",
		Category:   "未分类",
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
	}, nil
}

func (c *FeishuClient) Status(ctx context.Context, credentials []Credential) ([]Status, error) {
	if len(credentials) == 0 {
		return []Status{}, nil
	}
	if len(credentials) > maxStatusBatch {
		return nil, fmt.Errorf("feedback status batch is too large")
	}
	validCredentials := make([]Credential, 0, len(credentials))
	var itemErr error
	for _, credential := range credentials {
		credential.FeedbackID = strings.TrimSpace(credential.FeedbackID)
		credential.ReadToken = strings.TrimSpace(credential.ReadToken)
		if credential.FeedbackID == "" || credential.ReadToken == "" {
			itemErr = fmt.Errorf("%w: incomplete feedback credential", ErrRemoteUnavailable)
			continue
		}
		validCredentials = append(validCredentials, credential)
	}
	if len(validCredentials) == 0 {
		return nil, itemErr
	}
	cfg, err := c.loadConfig()
	if err != nil {
		return nil, err
	}
	token, err := c.tenantToken(ctx, cfg)
	if err != nil {
		return nil, err
	}
	baseToken, err := c.resolveBaseToken(ctx, cfg, token)
	if err != nil {
		return nil, err
	}
	fields, err := c.fields(ctx, cfg, baseToken, token)
	if err != nil {
		return nil, err
	}

	records, err := c.getRecords(ctx, cfg.TableID, baseToken, token, validCredentials)
	if err != nil {
		return nil, err
	}
	items := make([]Status, 0, len(validCredentials))
	for _, credential := range validCredentials {
		record, exists := records[credential.FeedbackID]
		if !exists {
			itemErr = fmt.Errorf("%w: record is unavailable", ErrRemoteUnavailable)
			continue
		}
		if fields["查询凭证摘要"] {
			expected := strings.TrimSpace(cellString(record.Fields["查询凭证摘要"]))
			actual := tokenDigest(credential.ReadToken)
			if expected == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) != 1 {
				itemErr = fmt.Errorf("%w: feedback credential mismatch", ErrRemoteUnavailable)
				continue
			}
		}
		status, statusErr := normalizeStatus(record.Fields)
		if statusErr != nil {
			itemErr = statusErr
			continue
		}
		items = append(items, Status{
			FeedbackID: credential.FeedbackID,
			Status:     status,
			Category:   defaultString(cellString(record.Fields["反馈类型"]), "未分类"),
			Reply:      cellString(record.Fields["开发者回复"]),
			UpdatedAt:  feishuTime(firstNonEmpty(record.UpdatedAt, record.UpdatedAt2), ""),
		})
	}
	if len(items) == 0 && itemErr != nil {
		return nil, itemErr
	}
	return items, nil
}

type feishuRecord struct {
	RecordID   string         `json:"record_id"`
	Fields     map[string]any `json:"fields"`
	UpdatedAt  string         `json:"last_modified_time"`
	UpdatedAt2 string         `json:"updated_time"`
}

func (c *FeishuClient) getRecords(
	ctx context.Context,
	tableID, baseToken, token string,
	credentials []Credential,
) (map[string]feishuRecord, error) {
	var response struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Records            []feishuRecord `json:"records"`
			ForbiddenRecordIDs []string       `json:"forbidden_record_ids"`
			AbsentRecordIDs    []string       `json:"absent_record_ids"`
		} `json:"data"`
	}
	recordIDs := make([]string, 0, len(credentials))
	for _, credential := range credentials {
		recordID := strings.TrimSpace(credential.FeedbackID)
		if recordID == "" || strings.TrimSpace(credential.ReadToken) == "" {
			return nil, fmt.Errorf("%w: incomplete feedback credential", ErrRemoteUnavailable)
		}
		recordIDs = append(recordIDs, recordID)
	}
	path := fmt.Sprintf(
		"/open-apis/bitable/v1/apps/%s/tables/%s/records/batch_get",
		url.PathEscape(baseToken),
		url.PathEscape(tableID),
	)
	body := map[string]any{
		"record_ids":       recordIDs,
		"with_shared_url":  false,
		"automatic_fields": false,
	}
	if err := c.doJSON(ctx, http.MethodPost, path, token, body, &response); err != nil {
		return nil, err
	}
	if response.Code != 0 {
		return nil, feishuCodeError(response.Code)
	}
	records := make(map[string]feishuRecord, len(response.Data.Records))
	for _, record := range response.Data.Records {
		if record.RecordID != "" && record.Fields != nil {
			records[record.RecordID] = record
		}
	}
	return records, nil
}

func (c *FeishuClient) loadConfig() (Config, error) {
	if c.store == nil {
		return Config{}, ErrNotConfigured
	}
	cfg, err := c.store.Load()
	if err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *FeishuClient) tenantToken(ctx context.Context, cfg Config) (string, error) {
	configID := tokenDigest(cfg.AppID + "\x00" + cfg.AppSecret)
	now := c.now()
	c.mu.Lock()
	if c.token != "" && c.tokenConfigID == configID && now.Before(c.tokenExpires) {
		token := c.token
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	var response struct {
		Code        int    `json:"code"`
		Msg         string `json:"msg"`
		Token       string `json:"tenant_access_token"`
		ExpiresIn   int    `json:"expire"`
		ExpiresInV2 int    `json:"expire_in"`
	}
	if err := c.doJSON(
		ctx,
		http.MethodPost,
		"/open-apis/auth/v3/tenant_access_token/internal",
		"",
		map[string]string{"app_id": cfg.AppID, "app_secret": cfg.AppSecret},
		&response,
	); err != nil {
		return "", err
	}
	if response.Code != 0 || strings.TrimSpace(response.Token) == "" {
		return "", feishuCodeError(response.Code)
	}
	expiresIn := response.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = response.ExpiresInV2
	}
	if expiresIn <= 120 {
		expiresIn = 7200
	}
	c.mu.Lock()
	c.token = response.Token
	c.tokenConfigID = configID
	c.tokenExpires = now.Add(time.Duration(expiresIn-60) * time.Second)
	c.mu.Unlock()
	return response.Token, nil
}

func (c *FeishuClient) resolveBaseToken(ctx context.Context, cfg Config, token string) (string, error) {
	if cfg.BaseToken != "" {
		return cfg.BaseToken, nil
	}
	configID := tokenDigest(cfg.AppID + "\x00" + cfg.WikiToken)
	c.mu.Lock()
	if c.baseToken != "" && c.baseConfigID == configID {
		baseToken := c.baseToken
		c.mu.Unlock()
		return baseToken, nil
	}
	c.mu.Unlock()

	var response struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Node struct {
				ObjectType  string `json:"obj_type"`
				ObjectToken string `json:"obj_token"`
			} `json:"node"`
		} `json:"data"`
	}
	path := "/open-apis/wiki/v2/spaces/get_node?token=" + url.QueryEscape(cfg.WikiToken)
	if err := c.doJSON(ctx, http.MethodGet, path, token, nil, &response); err != nil {
		return "", err
	}
	if response.Code != 0 {
		return "", feishuCodeError(response.Code)
	}
	if response.Data.Node.ObjectType != "bitable" || strings.TrimSpace(response.Data.Node.ObjectToken) == "" {
		return "", fmt.Errorf("%w: wiki node is not a Base", ErrSchemaMismatch)
	}
	c.mu.Lock()
	c.baseToken = response.Data.Node.ObjectToken
	c.baseConfigID = configID
	c.fieldNames = nil
	c.fieldConfigID = ""
	c.mu.Unlock()
	return response.Data.Node.ObjectToken, nil
}

func (c *FeishuClient) fields(
	ctx context.Context,
	cfg Config,
	baseToken, token string,
) (map[string]bool, error) {
	configID := tokenDigest(cfg.AppID + "\x00" + baseToken + "\x00" + cfg.TableID)
	c.mu.Lock()
	if c.fieldNames != nil && c.fieldConfigID == configID {
		fields := cloneFields(c.fieldNames)
		c.mu.Unlock()
		return fields, nil
	}
	c.mu.Unlock()

	fields := make(map[string]bool)
	pageToken := ""
	for page := 0; page < 5; page++ {
		var response struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
			Data struct {
				Items []struct {
					Name string `json:"field_name"`
				} `json:"items"`
				HasMore   bool   `json:"has_more"`
				PageToken string `json:"page_token"`
			} `json:"data"`
		}
		query := url.Values{"page_size": []string{"100"}}
		if pageToken != "" {
			query.Set("page_token", pageToken)
		}
		path := fmt.Sprintf(
			"/open-apis/bitable/v1/apps/%s/tables/%s/fields?%s",
			url.PathEscape(baseToken),
			url.PathEscape(cfg.TableID),
			query.Encode(),
		)
		if err := c.doJSON(ctx, http.MethodGet, path, token, nil, &response); err != nil {
			return nil, err
		}
		if response.Code != 0 {
			return nil, feishuCodeError(response.Code)
		}
		for _, item := range response.Data.Items {
			if name := strings.TrimSpace(item.Name); name != "" {
				fields[name] = true
			}
		}
		if !response.Data.HasMore {
			break
		}
		pageToken = strings.TrimSpace(response.Data.PageToken)
		if pageToken == "" {
			return nil, fmt.Errorf("%w: incomplete field pagination", ErrRemoteUnavailable)
		}
	}
	c.mu.Lock()
	c.fieldNames = cloneFields(fields)
	c.fieldConfigID = configID
	c.mu.Unlock()
	return fields, nil
}

func (c *FeishuClient) doJSON(
	ctx context.Context,
	method, path, token string,
	input, output any,
) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("%w: encode request", ErrRemoteUnavailable)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.baseURL, "/")+path, body)
	if err != nil {
		return fmt.Errorf("%w: create request", ErrRemoteUnavailable)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Alpha-Nexus-Feedback/1")
	if input != nil {
		request.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: request failed", ErrRemoteUnavailable)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%w: HTTP %d", ErrRemoteUnavailable, response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("%w: decode response", ErrRemoteUnavailable)
	}
	return nil
}

func randomID(prefix string, bytesCount int) (string, error) {
	value := make([]byte, bytesCount)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(value), nil
}

func randomUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		value[0:4],
		value[4:6],
		value[6:8],
		value[8:10],
		value[10:16],
	), nil
}

func tokenDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func setIfFieldExists(values map[string]any, fields map[string]bool, name string, value any) {
	if fields[name] {
		values[name] = value
	}
}

func cloneFields(fields map[string]bool) map[string]bool {
	cloned := make(map[string]bool, len(fields))
	for name, exists := range fields {
		cloned[name] = exists
	}
	return cloned
}

func normalizeStatus(fields map[string]any) (string, error) {
	raw := strings.TrimSpace(cellString(fields["处理状态"]))
	switch raw {
	case "", "待处理", "未处理", "待办":
		if done, ok := fields["是否处理完成"].(bool); ok && done {
			return "已完成", nil
		}
		return "待处理", nil
	case "处理中", "进行中":
		return "处理中", nil
	case "已完成", "已处理", "完成":
		return "已完成", nil
	case "不采纳", "拒绝", "已拒绝":
		return "不采纳", nil
	default:
		return "", fmt.Errorf("%w: unsupported feedback status", ErrSchemaMismatch)
	}
}

func cellString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := cellString(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, ""))
	case map[string]any:
		for _, key := range []string{"text", "name", "value"} {
			if text := cellString(typed[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

func feishuTime(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC().Format(time.RFC3339)
	}
	var milliseconds int64
	if _, err := fmt.Sscanf(value, "%d", &milliseconds); err == nil && milliseconds > 0 {
		if milliseconds < 1_000_000_000_000 {
			milliseconds *= 1000
		}
		return time.UnixMilli(milliseconds).UTC().Format(time.RFC3339)
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func feishuCodeError(code int) error {
	if code == 0 {
		return fmt.Errorf("%w: incomplete response", ErrRemoteUnavailable)
	}
	return fmt.Errorf("%w: Feishu code %d", ErrRemoteUnavailable, code)
}
