package notify

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"alpha-nexus-egolite/internal/db"
)

const offlineScanInterval = 10 * time.Minute

type ChannelStatus struct {
	Kind         string   `json:"kind"`
	Enabled      bool     `json:"enabled"`
	Configured   bool     `json:"configured"`
	SMTPHost     string   `json:"smtp_host,omitempty"`
	SMTPPort     int      `json:"smtp_port,omitempty"`
	SMTPSecurity string   `json:"smtp_security,omitempty"`
	SMTPUsername string   `json:"smtp_username,omitempty"`
	EmailFrom    string   `json:"email_from,omitempty"`
	EmailTo      []string `json:"email_to,omitempty"`
}

type Settings struct {
	ReminderIntervalHours int             `json:"reminder_interval_hours"`
	Channels              []ChannelStatus `json:"channels"`
}

type ChannelUpdate struct {
	Enabled      bool     `json:"enabled"`
	WebhookURL   string   `json:"webhook_url,omitempty"`
	SMTPHost     string   `json:"smtp_host,omitempty"`
	SMTPPort     int      `json:"smtp_port,omitempty"`
	SMTPSecurity string   `json:"smtp_security,omitempty"`
	SMTPUsername string   `json:"smtp_username,omitempty"`
	Password     string   `json:"password,omitempty"`
	EmailFrom    string   `json:"email_from,omitempty"`
	EmailTo      []string `json:"email_to,omitempty"`
}

type AccountStateChange struct {
	AccountID    int64
	PlatformCode string
	AccountName  string
	Previous     string
	Current      string
	ReasonCode   string
}

type deliveryTask struct {
	message   Message
	accountID int64
	onlyKind  string
	reminder  bool
	local     bool
}

type Service struct {
	db           *db.DB
	secrets      SecretStore
	sender       Sender
	queue        chan deliveryTask
	scanInterval time.Duration
	startOnce    sync.Once
}

func NewService(database *db.DB, secrets SecretStore, sender Sender) *Service {
	if sender == nil {
		sender = NewDefaultSender(defaultDeliveryTimeout)
	}
	return &Service{
		db: database, secrets: secrets, sender: sender,
		queue: make(chan deliveryTask, 64), scanInterval: offlineScanInterval,
	}
}

func (service *Service) Start(ctx context.Context, waitGroup *sync.WaitGroup) {
	if service == nil || service.db == nil || service.secrets == nil || ctx == nil || waitGroup == nil {
		return
	}
	service.startOnce.Do(func() {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			ticker := time.NewTicker(service.scanInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case task := <-service.queue:
					service.dispatch(ctx, task)
				case <-ticker.C:
					service.enqueueOfflineReminders()
				}
			}
		}()
	})
}

// NotifyAccountChange is intentionally non-blocking. Login observation is the
// source of truth and must never wait on network delivery or secure storage.
func (service *Service) NotifyAccountChange(change AccountStateChange) error {
	if service == nil || change.AccountID <= 0 {
		return newError(CodeInvalidConfig)
	}
	var task deliveryTask
	switch {
	case change.Previous == db.AccountAuthLoggedIn && change.Current == db.AccountAuthLoggedOut:
		task = deliveryTask{accountID: change.AccountID, local: true, message: offlineMessage(change, false)}
	case change.Previous == db.AccountAuthLoggedOut && change.Current == db.AccountAuthLoggedIn:
		task = deliveryTask{accountID: change.AccountID, local: true, message: recoveredMessage(change)}
	default:
		return nil
	}
	select {
	case service.queue <- task:
		return nil
	default:
		return newError(CodeQueueFull)
	}
}

func (service *Service) enqueueOfflineReminders() {
	accounts, err := service.db.ListAccounts()
	if err != nil {
		return
	}
	for _, account := range accounts {
		if !account.Enabled || account.LoginState != db.AccountAuthLoggedOut {
			continue
		}
		change := AccountStateChange{
			AccountID: account.ID, PlatformCode: account.PlatformCode, AccountName: account.Name,
			Previous: db.AccountAuthLoggedOut, Current: db.AccountAuthLoggedOut,
		}
		task := deliveryTask{accountID: account.ID, reminder: true, message: offlineMessage(change, true)}
		select {
		case service.queue <- task:
		default:
			return
		}
	}
}

func (service *Service) Settings() (*Settings, error) {
	if service == nil || service.db == nil || service.secrets == nil {
		return nil, newError(CodeInternal)
	}
	hours, err := service.db.NotificationReminderHours()
	if err != nil {
		return nil, newError(CodeInternal)
	}
	channels, err := service.db.ListNotificationChannels()
	if err != nil {
		return nil, newError(CodeInternal)
	}
	result := &Settings{ReminderIntervalHours: hours, Channels: make([]ChannelStatus, 0, len(channels))}
	for _, channel := range channels {
		configured, err := service.secrets.Configured(channel.Kind)
		if err != nil {
			return nil, err
		}
		result.Channels = append(result.Channels, channelStatus(channel, configured))
	}
	return result, nil
}

func (service *Service) SetReminderInterval(hours int) error {
	if service == nil || service.db == nil {
		return newError(CodeInternal)
	}
	if err := service.db.SetNotificationReminderHours(hours); err != nil {
		return newError(CodeInvalidConfig)
	}
	return nil
}

func (service *Service) UpdateChannel(ctx context.Context, kind string, update ChannelUpdate) (*ChannelStatus, error) {
	if service == nil || service.db == nil || service.secrets == nil || service.sender == nil || !validKind(kind) {
		return nil, newError(CodeInvalidConfig)
	}
	current, err := service.db.GetNotificationChannel(kind)
	if err != nil {
		return nil, newError(CodeInternal)
	}
	current.Enabled = update.Enabled
	secret := strings.TrimSpace(update.WebhookURL)
	if kind == "email" {
		secret = strings.TrimSpace(update.Password)
		current.SMTPHost = strings.TrimSpace(update.SMTPHost)
		current.SMTPPort = update.SMTPPort
		current.SMTPSecurity = strings.ToLower(strings.TrimSpace(update.SMTPSecurity))
		current.SMTPUsername = strings.TrimSpace(update.SMTPUsername)
		current.EmailFrom = strings.TrimSpace(update.EmailFrom)
		current.EmailTo = normalizeRecipients(update.EmailTo)
		if current.Enabled || secret != "" || current.SMTPHost != "" || len(current.EmailTo) > 0 {
			validationSecret := secret
			if validationSecret == "" {
				validationSecret = "configured-password"
			}
			if err := service.sender.Validate(ctx, *current, validationSecret); err != nil {
				return nil, err
			}
		}
	} else {
		current.SMTPHost, current.SMTPSecurity, current.SMTPUsername, current.EmailFrom = "", "", "", ""
		current.SMTPPort = 0
		current.EmailTo = []string{}
		if secret != "" {
			if err := service.sender.Validate(ctx, *current, secret); err != nil {
				return nil, err
			}
		}
	}
	configured, err := service.secrets.Configured(kind)
	if err != nil {
		return nil, err
	}
	if secret != "" {
		if err := service.secrets.Save(kind, secret); err != nil {
			return nil, err
		}
		configured = true
	}
	if current.Enabled && !configured {
		return nil, newError(CodeNotConfigured)
	}
	saved, err := service.db.SaveNotificationChannel(*current)
	if err != nil {
		return nil, newError(CodeInternal)
	}
	if saved.Enabled {
		service.enqueueOfflineReminders()
	}
	status := channelStatus(*saved, configured)
	return &status, nil
}

func normalizeRecipients(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func channelStatus(channel db.NotificationChannel, configured bool) ChannelStatus {
	status := ChannelStatus{Kind: channel.Kind, Enabled: channel.Enabled, Configured: configured}
	if channel.Kind == "email" {
		status.SMTPHost, status.SMTPPort, status.SMTPSecurity = channel.SMTPHost, channel.SMTPPort, channel.SMTPSecurity
		status.SMTPUsername, status.EmailFrom, status.EmailTo = channel.SMTPUsername, channel.EmailFrom, channel.EmailTo
	}
	return status
}

func (service *Service) TestChannel(ctx context.Context, kind string) (int64, error) {
	if !validKind(kind) {
		return 0, newError(CodeInvalidConfig)
	}
	return service.deliver(ctx, kind, deliveryTask{
		onlyKind: kind,
		message:  Message{EventType: "test", Title: "Alpha-Nexus 通知测试", Body: "通知已连接。账号掉线或恢复时，这里会收到提醒。"},
	})
}

func (service *Service) dispatch(ctx context.Context, task deliveryTask) {
	if task.local {
		level := "info"
		if task.message.EventType == "account_offline" {
			level = "warn"
		}
		_, _ = service.db.CreateNotification(task.message.EventType, level, task.message.Title, task.message.Body, "account", task.accountID)
	}
	channels, err := service.db.ListNotificationChannels()
	if err != nil {
		return
	}
	for _, channel := range channels {
		if !channel.Enabled || (task.onlyKind != "" && task.onlyKind != channel.Kind) {
			continue
		}
		if task.reminder {
			hours, err := service.db.NotificationReminderHours()
			if err != nil {
				continue
			}
			latest, found, err := service.db.LatestAccountNotificationAttempt(task.accountID, channel.Kind)
			if err != nil || (found && time.Since(latest) < time.Duration(hours)*time.Hour) {
				continue
			}
		}
		// 投递失败此前被静默丢弃：飞书/钉钉/webhook 发不出去时，用户既看不到
		// 任何提示，也不会在投递记录里留下失败原因。
		if _, deliverErr := service.deliver(ctx, channel.Kind, task); deliverErr != nil {
			log.Printf("notification deliver failed channel=%s account=%d event=%s error=%v",
				channel.Kind, task.accountID, task.message.EventType, deliverErr)
		}
	}
}

func (service *Service) deliver(ctx context.Context, kind string, task deliveryTask) (int64, error) {
	channel, err := service.db.GetNotificationChannel(kind)
	if err != nil {
		return 0, newError(CodeInternal)
	}
	configured, err := service.secrets.Configured(kind)
	if err != nil {
		return 0, err
	}
	if !configured {
		return 0, newError(CodeNotConfigured)
	}
	delivery, err := service.db.CreateNotificationDelivery(kind, task.message.EventType, task.accountID)
	if err != nil {
		return 0, newError(CodeInternal)
	}
	secret, err := service.secrets.Load(kind)
	if err == nil {
		deliveryCtx, cancel := context.WithTimeout(ctx, defaultDeliveryTimeout)
		err = service.sender.Send(deliveryCtx, *channel, secret, task.message)
		cancel()
	}
	code := CodeOf(err)
	if completeErr := service.db.CompleteNotificationDelivery(delivery.ID, string(code)); completeErr != nil {
		return delivery.ID, newError(CodeInternal)
	}
	if err != nil {
		return delivery.ID, err
	}
	return delivery.ID, nil
}

func offlineMessage(change AccountStateChange, reminder bool) Message {
	name := strings.TrimSpace(change.AccountName)
	if name == "" {
		name = "未命名账号"
	}
	prefix := platformName(change.PlatformCode)
	body := fmt.Sprintf("%s账号“%s”需要重新登录。%s", prefix, name, humanLogoutReason(change.ReasonCode))
	if reminder {
		body = fmt.Sprintf("%s账号“%s”仍未登录，请在方便时打开 Alpha-Nexus 重新登录。", prefix, name)
	}
	return Message{EventType: map[bool]string{true: "account_offline_reminder", false: "account_offline"}[reminder], Title: "账号需要重新登录", Body: body}
}

func recoveredMessage(change AccountStateChange) Message {
	name := strings.TrimSpace(change.AccountName)
	if name == "" {
		name = "未命名账号"
	}
	return Message{EventType: "account_recovered", Title: "账号已恢复登录", Body: fmt.Sprintf("%s账号“%s”已恢复，可以继续使用。", platformName(change.PlatformCode), name)}
}

func humanLogoutReason(reason string) string {
	switch reason {
	case db.LoginReasonSessionExpiredPage:
		return "登录已过期。"
	case db.LoginReasonCheckpointRequired:
		return "平台要求完成安全验证。"
	case db.LoginReasonLoginRequiredPage:
		return "平台已进入登录页面。"
	default:
		return "登录状态已失效。"
	}
}

func platformName(code string) string {
	switch code {
	case "kuaishou":
		return "快手"
	case "xhs":
		return "小红书"
	case "douyin":
		return "抖音"
	case "wechat_mp":
		return "微信公众号"
	case "wechat_video":
		return "视频号"
	case "csdn":
		return "CSDN"
	default:
		return ""
	}
}
