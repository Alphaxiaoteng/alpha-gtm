package notify

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/netip"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/db"
)

const defaultDeliveryTimeout = 8 * time.Second

type Message struct {
	EventType string
	Title     string
	Body      string
}

type Sender interface {
	Validate(ctx context.Context, channel db.NotificationChannel, secret string) error
	Send(ctx context.Context, channel db.NotificationChannel, secret string, message Message) error
}

type DefaultSender struct {
	httpClient         *http.Client
	providerHTTPClient *http.Client
	providerProxy      func(*http.Request) (*url.URL, error)
	timeout            time.Duration
}

func NewDefaultSender(timeout time.Duration) *DefaultSender {
	if timeout <= 0 {
		timeout = defaultDeliveryTimeout
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = safeDialContext
	providerProxy := http.ProxyFromEnvironment
	providerTransport := http.DefaultTransport.(*http.Transport).Clone()
	providerTransport.Proxy = providerProxy
	return &DefaultSender{
		timeout: timeout,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		// Official provider endpoints may need the operator's HTTPS proxy on
		// managed networks. The operator-provided proxy is trusted only for
		// these fixed hosts; generic Webhooks never use this client.
		providerHTTPClient: &http.Client{
			Timeout:   timeout,
			Transport: providerTransport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		providerProxy: providerProxy,
	}
}

func (sender *DefaultSender) Validate(ctx context.Context, channel db.NotificationChannel, secret string) error {
	if !validKind(channel.Kind) {
		return newError(CodeInvalidConfig)
	}
	if _, err := normalizeSecret(channel.Kind, secret); err != nil {
		return err
	}
	if channel.Kind == "email" {
		return validateEmailChannel(channel)
	}
	return validateWebhookURL(ctx, channel.Kind, secret, net.DefaultResolver)
}

func (sender *DefaultSender) Send(ctx context.Context, channel db.NotificationChannel, secret string, message Message) error {
	if sender == nil || sender.httpClient == nil || strings.TrimSpace(message.Title) == "" || strings.TrimSpace(message.Body) == "" {
		return newError(CodeInvalidConfig)
	}
	if err := sender.Validate(ctx, channel, secret); err != nil {
		return err
	}
	if channel.Kind == "email" {
		return sender.sendEmail(ctx, channel, secret, message)
	}
	return sender.sendWebhook(ctx, channel.Kind, secret, message)
}

type ipResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

func validateWebhookURL(ctx context.Context, kind, raw string, resolver ipResolver) error {
	if kind != "feishu" && kind != "dingtalk" && kind != "webhook" {
		return newError(CodeInvalidConfig)
	}
	if len(raw) > 4096 || strings.ContainsAny(raw, "\r\n\x00") {
		return newError(CodeEndpointRejected)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || parsed.Hostname() == "" {
		return newError(CodeEndpointRejected)
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return newError(CodeEndpointRejected)
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	switch kind {
	case "feishu":
		if host != "open.feishu.cn" || !strings.HasPrefix(parsed.EscapedPath(), "/open-apis/bot/v2/hook/") || strings.TrimPrefix(parsed.EscapedPath(), "/open-apis/bot/v2/hook/") == "" || parsed.RawQuery != "" {
			return newError(CodeEndpointRejected)
		}
	case "dingtalk":
		if host != "oapi.dingtalk.com" || parsed.EscapedPath() != "/robot/send" || parsed.Query().Get("access_token") == "" {
			return newError(CodeEndpointRejected)
		}
	case "webhook":
		if err := validatePublicHost(ctx, host, resolver); err != nil {
			return err
		}
	}
	return nil
}

func validatePublicHost(ctx context.Context, host string, resolver ipResolver) error {
	if ip := net.ParseIP(host); ip != nil {
		if !publicIP(ip) {
			return newError(CodeEndpointRejected)
		}
		return nil
	}
	if resolver == nil {
		return newError(CodeEndpointRejected)
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	addresses, err := resolver.LookupIPAddr(lookupCtx, host)
	if err != nil || len(addresses) == 0 {
		return newError(CodeEndpointRejected)
	}
	for _, address := range addresses {
		if !publicIP(address.IP) {
			return newError(CodeEndpointRejected)
		}
	}
	return nil
}

var specialUsePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

func publicIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range specialUsePrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func safeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, newError(CodeEndpointRejected)
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return nil, newError(CodeRequestFailed)
	}
	for _, address := range addresses {
		if !publicIP(address.IP) {
			return nil, newError(CodeEndpointRejected)
		}
	}
	dialer := &net.Dialer{Timeout: defaultDeliveryTimeout}
	for _, candidate := range addresses {
		connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
		if dialErr == nil {
			return connection, nil
		}
	}
	return nil, newError(CodeRequestFailed)
}

func (sender *DefaultSender) sendWebhook(ctx context.Context, kind, endpoint string, message Message) error {
	var payload any
	switch kind {
	case "feishu":
		payload = map[string]any{"msg_type": "text", "content": map[string]string{"text": message.Title + "\n" + message.Body}}
	case "dingtalk":
		payload = map[string]any{"msgtype": "text", "text": map[string]string{"content": message.Title + "\n" + message.Body}}
	case "webhook":
		payload = map[string]string{"event": message.EventType, "title": message.Title, "text": message.Body}
	default:
		return newError(CodeInvalidConfig)
	}
	encoded, err := json.Marshal(payload)
	if err != nil || len(encoded) > 16<<10 {
		return newError(CodeInvalidConfig)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(encoded)))
	if err != nil {
		return newError(CodeInvalidConfig)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	client, err := sender.webhookClient(kind, request)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return newError(CodeRequestFailed)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(responseBody) > 64<<10 {
		return newError(CodeProviderRejected)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return newError(CodeProviderRejected)
	}
	if kind == "feishu" || kind == "dingtalk" {
		var result struct {
			Code       *int `json:"code"`
			StatusCode *int `json:"StatusCode"`
			ErrCode    *int `json:"errcode"`
		}
		decoder := json.NewDecoder(strings.NewReader(string(responseBody)))
		if err := decoder.Decode(&result); err != nil {
			return newError(CodeProviderRejected)
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return newError(CodeProviderRejected)
		}
		if kind == "feishu" {
			valid := result.Code != nil && *result.Code == 0
			if result.Code == nil {
				valid = result.StatusCode != nil && *result.StatusCode == 0
			}
			if !valid {
				return newError(CodeProviderRejected)
			}
		} else if result.ErrCode == nil || *result.ErrCode != 0 {
			return newError(CodeProviderRejected)
		}
	}
	return nil
}

func (sender *DefaultSender) webhookClient(kind string, request *http.Request) (*http.Client, error) {
	if sender == nil {
		return nil, newError(CodeInvalidConfig)
	}
	if sender.httpClient == nil || request == nil {
		return nil, newError(CodeInvalidConfig)
	}
	if (kind != "feishu" && kind != "dingtalk") || sender.providerHTTPClient == nil || sender.providerProxy == nil {
		return sender.httpClient, nil
	}
	proxy, err := sender.providerProxy(request)
	if err != nil {
		return nil, newError(CodeRequestFailed)
	}
	if proxy == nil {
		return sender.providerHTTPClient, nil
	}
	if proxy.Host == "" || (proxy.Scheme != "http" && proxy.Scheme != "https") {
		return nil, newError(CodeRequestFailed)
	}
	// The first hop is an operator-configured proxy when present. With no
	// proxy, this same client connects only to the fixed, TLS-verified provider
	// host; generic Webhooks always remain on the direct safe client.
	return sender.providerHTTPClient, nil
}

func validateEmailChannel(channel db.NotificationChannel) error {
	host := strings.TrimSpace(channel.SMTPHost)
	if host == "" || strings.ContainsAny(host, " /@\r\n\x00") || channel.SMTPPort <= 0 || channel.SMTPPort > 65535 {
		return newError(CodeInvalidConfig)
	}
	if channel.SMTPSecurity != "tls" && channel.SMTPSecurity != "starttls" {
		return newError(CodeInvalidConfig)
	}
	if len(channel.EmailTo) == 0 || len(channel.EmailTo) > 20 || !plainEmailAddress(channel.EmailFrom) {
		return newError(CodeInvalidConfig)
	}
	for _, recipient := range channel.EmailTo {
		if !plainEmailAddress(recipient) {
			return newError(CodeInvalidConfig)
		}
	}
	if strings.ContainsAny(channel.SMTPUsername, "\r\n\x00") {
		return newError(CodeInvalidConfig)
	}
	return nil
}

func plainEmailAddress(value string) bool {
	value = strings.TrimSpace(value)
	parsed, err := mail.ParseAddress(value)
	return err == nil && parsed.Address == value && !strings.ContainsAny(value, "\r\n\x00")
}

func (sender *DefaultSender) sendEmail(ctx context.Context, channel db.NotificationChannel, password string, message Message) error {
	// SMTP is an explicit user-selected server and may intentionally be on a
	// private LAN. It uses a direct net.Dialer (no proxy) and still requires
	// verified TLS or STARTTLS; generic Webhook endpoints remain public-only.
	host := strings.TrimSpace(channel.SMTPHost)
	address := net.JoinHostPort(host, strconv.Itoa(channel.SMTPPort))
	dialer := &net.Dialer{Timeout: sender.timeout}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return newError(CodeSMTPFailed)
	}
	deadline := time.Now().Add(sender.timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = connection.SetDeadline(deadline)
	if channel.SMTPSecurity == "tls" {
		tlsConnection := tls.Client(connection, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host})
		if err := tlsConnection.HandshakeContext(ctx); err != nil {
			_ = connection.Close()
			return newError(CodeSMTPFailed)
		}
		connection = tlsConnection
	}
	client, err := smtp.NewClient(connection, host)
	if err != nil {
		_ = connection.Close()
		return newError(CodeSMTPFailed)
	}
	defer client.Close()
	if channel.SMTPSecurity == "starttls" {
		if err := client.StartTLS(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}); err != nil {
			return newError(CodeSMTPFailed)
		}
	}
	if channel.SMTPUsername != "" {
		if err := client.Auth(smtp.PlainAuth("", channel.SMTPUsername, password, host)); err != nil {
			return newError(CodeSMTPFailed)
		}
	}
	if err := client.Mail(channel.EmailFrom); err != nil {
		return newError(CodeSMTPFailed)
	}
	for _, recipient := range channel.EmailTo {
		if err := client.Rcpt(recipient); err != nil {
			return newError(CodeSMTPFailed)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return newError(CodeSMTPFailed)
	}
	mailBody := buildEmailMessage(channel, message)
	if _, err := io.WriteString(writer, mailBody); err != nil {
		_ = writer.Close()
		return newError(CodeSMTPFailed)
	}
	if err := writer.Close(); err != nil {
		return newError(CodeSMTPFailed)
	}
	if err := client.Quit(); err != nil {
		return newError(CodeSMTPFailed)
	}
	return nil
}

func sanitizeHeader(value string) string {
	return strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(value))
}

func buildEmailMessage(channel db.NotificationChannel, message Message) string {
	return fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n",
		channel.EmailFrom, strings.Join(channel.EmailTo, ", "), mime.QEncoding.Encode("UTF-8", sanitizeHeader(message.Title)), message.Body)
}
