// Package skilllib owns the private, local Skill catalog used by the UI and
// Agent prompt resolver. Executable product actions remain in tools.Registry.
package skilllib

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/db"
)

const (
	SourceSystem = "system"
	SourceLocal  = "local"

	StatusReviewed       = "reviewed"
	StatusReady          = "ready"
	StatusReviewRequired = "review_required"

	RuntimeInstructions = "instructions"
	RuntimeRegistry     = "registry"
)

var (
	ErrNotFound      = errors.New("skill not found")
	ErrNotEnableable = errors.New("skill requires review before enablement")
	skillIDPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)
)

type Skill struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions,omitempty"`
	Source       string `json:"source"`
	Status       string `json:"status"`
	Runtime      string `json:"runtime"`
	RuntimeRef   string `json:"runtime_ref,omitempty"`
	Enabled      bool   `json:"enabled"`
	HasScripts   bool   `json:"has_scripts"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	directory    string
}

type CreateInput struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
	Automation   bool   `json:"automation"`
}

type Library struct {
	db   *db.DB
	root string
	now  func() time.Time
}

type seedSkill struct {
	ID, Name, Description, Instructions, Runtime, RuntimeRef string
}

var builtins = []seedSkill{
	{
		ID: "ego-browser", Name: "ego-browser 1.2.3",
		Description:  "官方 ego-browser Space 工作流的内置安全说明与能力边界。",
		Instructions: `Use Alpha-Nexus EgoLite Registry tools only. Treat one matrix as one isolated persistent Space shared by its platform accounts. Respect ownership: after a login or verification handoff, stop work on that Space until the user explicitly confirms resume. Never expose authentication storage, local runtime paths, or raw browser internals. Use social publish preparation tools for reviewed content operations; final submission always requires frozen-batch approval and must happen at most once per target.`,
		Runtime:      RuntimeInstructions,
	},
	{
		ID: "alpha-nexus-egolite-matrix", Name: "EgoLite 浏览器账号",
		Description:  "创建和配置浏览器、导入平台收藏夹、分批登录、核验资料与检查会话。",
		Instructions: `Operate matrices only through reviewed Alpha-Nexus Registry capabilities. Keep the local web console as the management UI and EgoLite as the platform-page runtime. Create or rename a matrix without exposing Space IDs. Configure one account per platform and reuse one persistent Space per matrix. Open at most three creator backends per login batch, then hand control to the user and stop until explicit confirmation. After confirmation, verify profile DOM evidence and persist only reliable platform name, account ID, avatar, and login state. Preserve the previous state when evidence is unknown; hand control back on CAPTCHA, QR, 2FA, or checkpoint. Import bookmarks only through the reviewed standard-HTML flow and never edit EgoLite Default, cookies, passwords, or Profile files. Never launch another browser runtime, use arbitrary shell access, or submit content without frozen-batch approval.`,
		Runtime:      RuntimeInstructions,
	},
	{
		ID: "skill-creator", Name: "Skill Creator",
		Description:  "通过 Agent 对话设计一个极简、本地优先的标准 Skill。",
		Instructions: `Act as the Alpha-Nexus Skill Creator. Clarify the concrete trigger and smallest useful workflow. Return one complete SKILL.md with YAML frontmatter fields name and description followed by imperative instructions. Prefer existing Registry tools and native APIs. Add scripts only for deterministic repeated work. Never include credentials, profile access, raw CDP, final publish clicks, or claims of unverified automation. When the Skill needs browser, Playwright, account, or publishing code, mark it as requiring developer review and keep the proposed script fixed, minimal, and local. In the final creation response, output the complete SKILL.md without surrounding commentary so it can be added to the local library.`,
		Runtime:      RuntimeInstructions,
	},
	{
		ID: "social-content-workflow", Name: "社媒内容工作流",
		Description:  "基于已提供事实完成选题、文案、脚本与审校，保留证据边界。",
		Instructions: `Follow an evidence-first social content workflow. Keep the objective, audience, platform, tone, approved facts, source notes, assets, and claim gaps explicit. For topic work, offer distinct angles supported by the supplied evidence. For copy and scripts, adapt to the requested platform and keep unsupported claims as opinions, questions, or approval-required gaps. Never invent performance metrics, dates, prices, availability, endorsements, customer cases, or outcomes. Return concise content plus an evidence ledger and review blockers when applicable. This skill does not grant tools, permissions, browser access, credentials, or publishing authority.`,
		Runtime:      RuntimeInstructions,
	},
	{
		ID: "one-click-publish", Name: "一键发布（计划与预填）",
		Description:  "将多平台发布收敛为能力预检、冻结计划、状态读取和独立授权的预填编排；最终提交始终分离。",
		Instructions: `Use this as a reviewed Registry orchestration recipe, not as a macro. First call social.publish-capabilities to report the declared platform/content boundary without opening a Space. For phase=plan, call only publish.plan.freeze with the complete frozen-plan input: content_id, media_mode, media_ids, targets, and any applicable optional freeze fields. Do not create a browser or run prepare in the same step. For phase=status, call publish.plan.get for one plan or publish.plan.list for local summaries only. For phase=prepare, use an existing plan_id and its unchanged snapshot_hash, then propose publish.plan.prepare. The Agent/domain approval path injects approval_id and nonce; never create, request, retain, or provide those credentials in model arguments. Stop after the prepare result, including LOGIN_REQUIRED, pending verification, or failure. Never call publish.plan.submit, social.publish-submit-approved, a final platform control, arbitrary browser automation, shell commands, or a scheduling workflow. Prepared means prepared_for_review, never published.`,
		Runtime:      RuntimeInstructions,
	},
	{
		ID: "video-publisher", Name: "Video Publisher",
		Description:  "按上游四平台的草稿与 READY 思路准备视频发布内容，但只接入 Alpha-Nexus 预填流程。",
		Instructions: `Use the Alpha-Nexus Registry only, through the top-level publish.plan.prepare tool for an explicitly frozen plan. The upstream four-platform draft/READY workflow is reference material for inspect, prefill, and verify sequencing only: READY means a verified draft or prepared form, never a published result. Do not call a submit operation, click a final publish control, invoke an external video-publisher runtime, run arbitrary scripts, or access browser internals. The Agent/domain approval path injects approval_id and nonce; never request, retain, or provide them. Preserve the snapshot_hash, frozen plan, login-required boundaries, and failure evidence.`,
		Runtime:      RuntimeInstructions,
	},
	{
		ID: "multipost", Name: "MultiPost",
		Description:  "参考 MultiPost 的平台映射与字段适配准备发布草稿，不接入浏览器扩展运行时。",
		Instructions: `Use the Alpha-Nexus Registry only, through the top-level publish.plan.prepare tool for an explicitly frozen plan. MultiPost is reference material for platform mapping and content-field adaptation only; it is not an executable browser extension or publishing runtime here. READY or prefilled fields never mean published. Do not invoke a Chrome extension, direct browser automation, arbitrary script, submit operation, or final publish click. The Agent/domain approval path injects approval_id and nonce; never request, retain, or provide them. Preserve the snapshot_hash, frozen plan, login-required boundaries, and failure evidence.`,
		Runtime:      RuntimeInstructions,
	},
	{
		ID: "social-account-check", Name: "账号登录检查",
		Description:  "对明确选择且已运行的账号做被动登录检查。",
		Instructions: "Use only the reviewed social.account-check Registry workflow for explicit account IDs. Do not navigate, read cookies, access profiles, or infer login success without returned evidence.",
		Runtime:      RuntimeRegistry, RuntimeRef: "social.account-check",
	},
	{
		ID: "social-materials-list", Name: "本地素材清单",
		Description:  "读取已入库的视频、图片和文章脚本索引。",
		Instructions: "Use only the reviewed social.materials-list Registry workflow. Read indexed local material records only and never scan arbitrary user paths.",
		Runtime:      RuntimeRegistry, RuntimeRef: "social.materials-list",
	},
	{
		ID: "social-metrics-collect", Name: "创作者后台数据采集",
		Description:  "串行读取明确选择账号的近 7 天指标并保存本地快照。",
		Instructions: "Use only the reviewed social.metrics.collect-batch and social.metrics.overview Registry workflows for explicit account IDs. Keep one Space serialized, restore the prior page, hand control back immediately, and preserve unknown instead of inventing metrics.",
		Runtime:      RuntimeRegistry, RuntimeRef: "social.metrics.collect-batch",
	},
	{
		ID: "social-publish-prepare", Name: "多平台发布预填",
		Description:  "按已冻结方案预填发布后台并停在最终发布前。",
		Instructions: "Use only the top-level publish.plan.prepare Registry tool after the trusted local UI approval for an unchanged frozen plan. The Agent/domain approval path injects approval_id and nonce; never request, retain, or provide them. Stop before the final publish action and preserve login-required, screenshot, and error evidence.",
		Runtime:      RuntimeInstructions,
	},
	{
		ID: "social-publish-submit-approved", Name: "已批准的最终发布",
		Description:  "使用独立单次授权提交已冻结且已准备的发布目标。",
		Instructions: "Use only the top-level publish.plan.submit Registry tool after a distinct trusted local UI approval for the unchanged frozen plan. The Agent/domain approval path injects approval_id and nonce; never request, retain, or provide them. Click at most once per target, never retry an uncertain result, and never claim success without reliable creator-backend evidence.",
		Runtime:      RuntimeInstructions,
	},
}

func New(database *db.DB, dataDir string) (*Library, error) {
	if database == nil || strings.TrimSpace(dataDir) == "" {
		return nil, fmt.Errorf("skill library requires database and data directory")
	}
	root := filepath.Join(dataDir, "skills")
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, fmt.Errorf("create skill directory: %w", err)
	}
	if err := os.Chmod(root, 0700); err != nil {
		return nil, fmt.Errorf("restrict skill directory: %w", err)
	}
	library := &Library{db: database, root: root, now: time.Now}
	if err := library.seed(context.Background()); err != nil {
		return nil, err
	}
	return library, nil
}

func (l *Library) Root() string { return l.root }

func (l *Library) List(ctx context.Context) ([]Skill, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT id,name,description,source,status,runtime,runtime_ref,directory,enabled,has_scripts,created_at,updated_at FROM skills ORDER BY source,id`)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	defer rows.Close()
	items := make([]Skill, 0)
	for rows.Next() {
		skill, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, skill)
	}
	return items, rows.Err()
}

func (l *Library) Get(ctx context.Context, id string) (*Skill, error) {
	skill, err := scanSkill(l.db.QueryRowContext(ctx, `SELECT id,name,description,source,status,runtime,runtime_ref,directory,enabled,has_scripts,created_at,updated_at FROM skills WHERE id=?`, strings.TrimSpace(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get skill: %w", err)
	}
	if skill.directory != skill.ID || !skillIDPattern.MatchString(skill.directory) {
		return nil, fmt.Errorf("invalid skill directory")
	}
	packageDir := filepath.Join(l.root, skill.directory)
	if info, statErr := os.Lstat(packageDir); statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("invalid skill package directory")
	}
	skillPath := filepath.Join(packageDir, "SKILL.md")
	if info, statErr := os.Lstat(skillPath); statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("invalid skill package file")
	}
	raw, err := os.ReadFile(skillPath)
	if err != nil {
		return nil, fmt.Errorf("read skill package: %w", err)
	}
	_, _, body, err := parseSkillMarkdown(string(raw))
	if err != nil {
		return nil, err
	}
	skill.Instructions = body
	return &skill, nil
}

func (l *Library) Create(ctx context.Context, input CreateInput) (*Skill, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Instructions = strings.TrimSpace(input.Instructions)
	if err := validateInput(input); err != nil {
		return nil, err
	}
	status := StatusReady
	if input.Automation {
		status = StatusReviewRequired
	}
	return l.create(ctx, Skill{
		ID: input.ID, Name: input.Name, Description: input.Description,
		Source: SourceLocal, Status: status, Runtime: RuntimeInstructions,
		Enabled: false, HasScripts: input.Automation,
	}, input.Instructions, false)
}

func (l *Library) Import(ctx context.Context, raw string) (*Skill, error) {
	raw = stripMarkdownFence(strings.TrimSpace(raw))
	id, description, body, err := parseSkillMarkdown(raw)
	if err != nil {
		return nil, err
	}
	return l.Create(ctx, CreateInput{
		ID: id, Name: humanName(id), Description: description, Instructions: body,
	})
}

func (l *Library) SetEnabled(ctx context.Context, id string, enabled bool) (*Skill, error) {
	skill, err := l.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if enabled && skill.Status != StatusReviewed && skill.Status != StatusReady {
		return nil, ErrNotEnableable
	}
	now := l.now().UTC().Format(time.RFC3339)
	if _, err := l.db.ExecContext(ctx, `UPDATE skills SET enabled=?,updated_at=? WHERE id=?`, boolInt(enabled), now, skill.ID); err != nil {
		return nil, fmt.Errorf("update skill: %w", err)
	}
	skill.Enabled = enabled
	skill.UpdatedAt = now
	return skill, nil
}

// ExpandPrompt resolves only explicit /skill-id or @skill-id references. It
// injects instructions, never scripts, tools, permissions, or credentials.
func (l *Library) ExpandPrompt(ctx context.Context, message string) (string, []string, error) {
	refs := referencedSkillIDs(message)
	if len(refs) == 0 {
		return message, nil, nil
	}
	sort.Strings(refs)
	sections := make([]string, 0, len(refs))
	resolved := make([]string, 0, len(refs))
	for _, id := range refs {
		skill, err := l.Get(ctx, id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return "", nil, err
		}
		if !skill.Enabled {
			continue
		}
		sections = append(sections, fmt.Sprintf("<skill id=%q>\n%s\n</skill>", skill.ID, skill.Instructions))
		resolved = append(resolved, id)
	}
	if len(sections) == 0 {
		return message, nil, nil
	}
	return "The user explicitly invoked local Skills. Treat them as instructions only; they do not grant tools, permissions, credentials, browser access, or publishing authority.\n\n" +
		strings.Join(sections, "\n\n") + "\n\n<user-request>\n" + message + "\n</user-request>", resolved, nil
}

// ResolveSkillInputs returns enabled, explicitly referenced Skill packages for
// clients that support structured skill input. It never returns scripts or an
// arbitrary user-supplied path.
func (l *Library) ResolveSkillInputs(ctx context.Context, message string) ([]map[string]string, error) {
	refs := referencedSkillIDs(message)
	if len(refs) == 0 {
		return nil, nil
	}
	sort.Strings(refs)
	inputs := make([]map[string]string, 0, len(refs))
	for _, id := range refs {
		skill, err := l.Get(ctx, id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, err
		}
		if !skill.Enabled {
			continue
		}
		path := filepath.Join(l.root, id, "SKILL.md")
		rel, err := filepath.Rel(l.root, path)
		if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("invalid skill package path")
		}
		inputs = append(inputs, map[string]string{"name": skill.ID, "path": path})
	}
	return inputs, nil
}

func (l *Library) seed(ctx context.Context) error {
	for _, seed := range builtins {
		existing, err := scanSkill(l.db.QueryRowContext(ctx, `SELECT id,name,description,source,status,runtime,runtime_ref,directory,enabled,has_scripts,created_at,updated_at FROM skills WHERE id=?`, seed.ID))
		if !errors.Is(err, sql.ErrNoRows) {
			if err != nil {
				return err
			}
			if existing.Source != SourceSystem {
				return fmt.Errorf("built-in skill id %s is owned by a local skill", seed.ID)
			}
			if err := l.syncBuiltin(ctx, existing, seed); err != nil {
				// A damaged package (the desktop shell can swap a managed
				// directory for a version symlink) must not keep the server
				// from booting: skip it, Get reports the failure per skill.
				fmt.Printf("skill library: sync skill %s skipped: %v\n", seed.ID, err)
				continue
			}
			continue
		}
		_, err = l.create(ctx, Skill{
			ID: seed.ID, Name: seed.Name, Description: seed.Description,
			Source: SourceSystem, Status: StatusReviewed, Runtime: seed.Runtime,
			RuntimeRef: seed.RuntimeRef, Enabled: true,
		}, seed.Instructions, true)
		if err != nil {
			return fmt.Errorf("seed skill %s: %w", seed.ID, err)
		}
	}
	return nil
}

func (l *Library) syncBuiltin(ctx context.Context, existing Skill, seed seedSkill) error {
	desired := skillMarkdown(seed.ID, seed.Description, seed.Instructions)
	packageDir := filepath.Join(l.root, seed.ID)
	info, err := os.Lstat(packageDir)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(packageDir, 0700); err != nil {
			return err
		}
	} else if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("invalid skill package directory")
	}
	skillPath := filepath.Join(packageDir, "SKILL.md")
	if info, statErr := os.Lstat(skillPath); statErr == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return fmt.Errorf("invalid skill package file")
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	raw, readErr := os.ReadFile(skillPath)
	fileChanged := readErr != nil || !bytes.Equal(raw, desired)
	metadataChanged := existing.Name != seed.Name || existing.Description != seed.Description ||
		existing.Status != StatusReviewed || existing.Runtime != seed.Runtime || existing.RuntimeRef != seed.RuntimeRef ||
		existing.directory != seed.ID || existing.HasScripts
	if !fileChanged && !metadataChanged {
		return nil
	}
	if fileChanged {
		if err := writePrivateAtomic(skillPath, desired); err != nil {
			return err
		}
	}
	now := l.now().UTC().Format(time.RFC3339)
	result, err := l.db.ExecContext(ctx, `UPDATE skills SET name=?,description=?,status=?,runtime=?,runtime_ref=?,directory=?,has_scripts=0,updated_at=? WHERE id=? AND source=?`,
		seed.Name, seed.Description, StatusReviewed, seed.Runtime, seed.RuntimeRef, seed.ID, now, seed.ID, SourceSystem)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return fmt.Errorf("system skill catalog update failed")
	}
	return nil
}

func (l *Library) create(ctx context.Context, skill Skill, instructions string, overwrite bool) (*Skill, error) {
	directory := skill.ID
	packageDir := filepath.Join(l.root, directory)
	if !overwrite {
		var exists int
		if err := l.db.QueryRowContext(ctx, `SELECT 1 FROM skills WHERE id=?`, skill.ID).Scan(&exists); err == nil {
			return nil, fmt.Errorf("skill %s already exists", skill.ID)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	if err := os.MkdirAll(packageDir, 0700); err != nil {
		return nil, fmt.Errorf("create skill package: %w", err)
	}
	if info, err := os.Lstat(packageDir); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("invalid skill package directory")
	}
	if err := writePrivateAtomic(filepath.Join(packageDir, "SKILL.md"), skillMarkdown(skill.ID, skill.Description, instructions)); err != nil {
		return nil, err
	}
	now := l.now().UTC().Format(time.RFC3339)
	skill.directory = directory
	skill.CreatedAt = now
	skill.UpdatedAt = now
	_, err := l.db.ExecContext(ctx, `INSERT INTO skills(id,name,description,source,status,runtime,runtime_ref,directory,enabled,has_scripts,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		skill.ID, skill.Name, skill.Description, skill.Source, skill.Status, skill.Runtime, skill.RuntimeRef, skill.directory, boolInt(skill.Enabled), boolInt(skill.HasScripts), now, now)
	if err != nil {
		return nil, fmt.Errorf("catalog skill: %w", err)
	}
	skill.Instructions = strings.TrimSpace(instructions)
	return &skill, nil
}

func skillMarkdown(id, description, instructions string) []byte {
	return []byte(fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s\n", id, oneLine(description), strings.TrimSpace(instructions)))
}

func scanSkill(scanner interface{ Scan(...any) error }) (Skill, error) {
	var skill Skill
	var enabled, hasScripts int
	err := scanner.Scan(&skill.ID, &skill.Name, &skill.Description, &skill.Source, &skill.Status, &skill.Runtime, &skill.RuntimeRef, &skill.directory, &enabled, &hasScripts, &skill.CreatedAt, &skill.UpdatedAt)
	skill.Enabled = enabled != 0
	skill.HasScripts = hasScripts != 0
	return skill, err
}

func validateInput(input CreateInput) error {
	if !skillIDPattern.MatchString(input.ID) {
		return fmt.Errorf("skill id must use 2-64 lowercase letters, numbers, or hyphens")
	}
	if input.Name == "" || len([]rune(input.Name)) > 80 {
		return fmt.Errorf("skill name is required and must be at most 80 characters")
	}
	if input.Description == "" || len([]rune(input.Description)) > 400 {
		return fmt.Errorf("skill description is required and must be at most 400 characters")
	}
	if input.Instructions == "" || len(input.Instructions) > 32*1024 {
		return fmt.Errorf("skill instructions are required and must be at most 32 KiB")
	}
	return nil
}

func parseSkillMarkdown(raw string) (string, string, string, error) {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	if len(lines) < 5 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", "", fmt.Errorf("SKILL.md requires YAML frontmatter")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return "", "", "", fmt.Errorf("SKILL.md frontmatter is not closed")
	}
	values := map[string]string{}
	for _, line := range lines[1:end] {
		key, value, ok := strings.Cut(line, ":")
		if ok {
			values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	id := values["name"]
	description := values["description"]
	body := strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	if err := validateInput(CreateInput{ID: id, Name: humanName(id), Description: description, Instructions: body}); err != nil {
		return "", "", "", err
	}
	return id, description, body, nil
}

func stripMarkdownFence(raw string) string {
	if !strings.HasPrefix(raw, "```") {
		return raw
	}
	lines := strings.Split(raw, "\n")
	if len(lines) >= 3 && strings.HasPrefix(lines[0], "```") && strings.TrimSpace(lines[len(lines)-1]) == "```" {
		return strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
	}
	return raw
}

func referencedSkillIDs(message string) []string {
	seen := map[string]bool{}
	refs := make([]string, 0)
	for _, field := range strings.Fields(message) {
		field = strings.Trim(field, "\"'`()[]{}<>，。,.!?！？:：;；")
		if len(field) < 3 || (field[0] != '/' && field[0] != '@') {
			continue
		}
		id := field[1:]
		if skillIDPattern.MatchString(id) && !seen[id] {
			seen[id] = true
			refs = append(refs, id)
		}
	}
	return refs
}

func humanName(id string) string {
	parts := strings.Split(id, "-")
	for i := range parts {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, " ")
}

func oneLine(value string) string { return strings.Join(strings.Fields(value), " ") }
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func writePrivateAtomic(path string, content []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".skill-*.tmp")
	if err != nil {
		return fmt.Errorf("create skill temp file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(content); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("install skill file: %w", err)
	}
	return os.Chmod(path, 0600)
}
